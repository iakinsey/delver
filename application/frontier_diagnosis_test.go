package application

// Opt-in diagnostic experiments, not production scheduling changes.
// DELVER_DIAG_ROOT=/private/path DELVER_FRONTIER_DIAG=1 go test ./application \
//   -run '^TestFrontierDiagnosisPipeline/baseline$' -count=1 -v -timeout=5m
// Select another named subtest to change exactly one experimental factor.
// DELVER_DIAG_REPLAY=/private/copy DELVER_DIAG_DEFINITION=/private/definition.json \
//   go test ./application -run '^TestFrontierDiagnosisReplay$' -count=1 -v

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/armon/go-metrics"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/worker"
	"github.com/iakinsey/delver/worker/frontierworker"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type diagnosisTimes struct {
	mu     sync.Mutex
	values map[string][]float64
	errors map[string]int
}

func (m *diagnosisTimes) measure(ctx context.Context, name string, fn func() error) error {
	started := time.Now()
	var err error
	pprof.Do(ctx, pprof.Labels("frontier_phase", name), func(ctx context.Context) {
		trace.WithRegion(ctx, name, func() { err = fn() })
	})
	m.mu.Lock()
	m.values[name] = append(m.values[name], float64(time.Since(started))/1e6)
	if err != nil {
		if m.errors == nil {
			m.errors = map[string]int{}
		}
		m.errors[name]++
	}
	m.mu.Unlock()
	return err
}

func (m *diagnosisTimes) snapshot() map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]interface{}{}
	for name, values := range m.values {
		v := append([]float64{}, values...)
		sort.Float64s(v)
		var total float64
		for _, n := range v {
			total += n
		}
		out[name] = map[string]interface{}{"calls": len(v), "errors_including_shutdown_cancellation": m.errors[name], "inclusive_wall_ms": total,
			"mean_ms": total / float64(len(v)), "p50_ms": v[(len(v)-1)/2],
			"p95_ms": v[(len(v)-1)*95/100], "max_ms": v[len(v)-1]}
	}
	return out
}

type diagnosisStore struct {
	frontier.FrontierStore
	ctx                     context.Context
	caller                  string
	times                   *diagnosisTimes
	store                   *frontier.Store
	publisherOnly, skipFull bool
	boundCapacity           bool
	deferDrain              bool
	deferredLimit           int
	capacity                int64
}

type diagnosisQueue struct {
	queue.Queue
	times *diagnosisTimes
}

func (q *diagnosisQueue) Put(m types.Message, priority int) error {
	return q.PutContext(context.Background(), m, priority)
}
func (q *diagnosisQueue) PutContext(ctx context.Context, m types.Message, priority int) error {
	return q.times.measure(ctx, "publish.enqueue", func() error { return queue.PutContext(ctx, q.Queue, m, priority) })
}

type diagnosisWorker struct {
	worker.Worker
	times  *diagnosisTimes
	caller string
}

func (w *diagnosisWorker) OnMessage(m types.Message) (interface{}, error) {
	return w.OnMessageContext(context.Background(), m)
}
func (w *diagnosisWorker) OnMessageContext(ctx context.Context, m types.Message) (interface{}, error) {
	var value interface{}
	err := w.times.measure(ctx, w.caller+".message", func() error {
		var err error
		if target, ok := w.Worker.(worker.ContextWorker); ok {
			value, err = target.OnMessageContext(ctx, m)
		} else {
			value, err = w.Worker.OnMessage(m)
		}
		return err
	})
	return value, err
}
func (w *diagnosisWorker) SetFlow(c *flow.Controller, p flow.Policy) {
	if target, ok := w.Worker.(worker.FlowAware); ok {
		target.SetFlow(c, p)
	}
}
func (w *diagnosisWorker) SetMetricSink(s metrics.MetricSink) {
	if target, ok := w.Worker.(worker.MetricAware); ok {
		target.SetMetricSink(s)
	}
}

func (s *diagnosisStore) StageDiscovery(ctx context.Context, id string, rs []message.FetcherRequest) error {
	return s.times.measure(ctx, s.caller+".stage", func() error { return s.FrontierStore.StageDiscovery(ctx, id, rs) })
}
func (s *diagnosisStore) DrainDiscovery(ctx context.Context, limit int) (int, error) {
	var n int
	err := s.times.measure(ctx, s.caller+".drain", func() error {
		if s.deferDrain {
			s.deferredLimit = limit
			return nil
		}
		if s.publisherOnly && s.caller != "publish" {
			return nil
		}
		if s.skipFull || s.boundCapacity {
			counts, err := s.store.Counts()
			if err != nil {
				return err
			}
			if counts["pending_and_active"] >= s.capacity {
				return nil
			}
			if s.boundCapacity {
				if free := s.capacity - counts["pending_and_active"]; int64(limit) > free {
					limit = int(free)
				}
			}
		}
		var err error
		n, err = s.FrontierStore.DrainDiscovery(ctx, limit)
		return err
	})
	return n, err
}

// Test-only ordering intervention: defer the existing admission call until
// after the unmodified publisher has selected and enqueued existing work.
type diagnosisPublishFirst struct {
	*frontierworker.Worker
	store *diagnosisStore
}

func (w *diagnosisPublishFirst) OnMessage(m types.Message) (interface{}, error) {
	return w.OnMessageContext(context.Background(), m)
}
func (w *diagnosisPublishFirst) OnMessageContext(ctx context.Context, m types.Message) (interface{}, error) {
	w.store.deferDrain = true
	w.store.deferredLimit = 0
	value, err := w.Worker.OnMessageContext(ctx, m)
	w.store.deferDrain = false
	if err == nil && w.store.deferredLimit > 0 {
		err = w.store.times.measure(ctx, "publish.drain_after", func() error { _, e := w.store.FrontierStore.DrainDiscovery(ctx, w.store.deferredLimit); return e })
	}
	return value, err
}
func (s *diagnosisStore) Outcome(r message.FetcherResponse) error {
	return s.times.measure(s.ctx, s.caller+".outcome", func() error { return s.FrontierStore.Outcome(r) })
}
func (s *diagnosisStore) Candidates(ctx context.Context, strategy, last string, limit int) ([]frontier.Candidate, error) {
	var rows []frontier.Candidate
	err := s.times.measure(ctx, s.caller+".candidates", func() error {
		var err error
		rows, err = s.FrontierStore.Candidates(ctx, strategy, last, limit)
		return err
	})
	return rows, err
}
func (s *diagnosisStore) Reserve(ctx context.Context, c frontier.Candidate) (frontier.Candidate, error) {
	var out frontier.Candidate
	err := s.times.measure(ctx, s.caller+".reserve", func() error { var err error; out, err = s.FrontierStore.Reserve(ctx, c); return err })
	return out, err
}
func (s *diagnosisStore) Published(c frontier.Candidate) error {
	return s.times.measure(s.ctx, s.caller+".published", func() error { return s.FrontierStore.Published(c) })
}
func (s *diagnosisStore) DrainRetries(ctx context.Context, limit int) error {
	return s.times.measure(ctx, s.caller+".retry", func() error { return s.store.DrainRetries(ctx, limit) })
}
func (s *diagnosisStore) DrainRecrawls(ctx context.Context, limit int) error {
	return s.times.measure(ctx, s.caller+".recrawl", func() error { return s.store.DrainRecrawls(ctx, limit) })
}

func diagnosisWrite(t *testing.T, name string, value interface{}) {
	t.Helper()
	root := os.Getenv("DELVER_DIAG_ROOT")
	if root == "" {
		t.Fatal("DELVER_DIAG_ROOT must name private evidence storage")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, name+".json"), append(b, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("evidence: %s", filepath.Join(root, name+".json"))
}

func diagnosisWorkDir(t *testing.T) string {
	t.Helper()
	base := os.Getenv("DELVER_DIAG_ROOT")
	if base == "" {
		t.Fatal("DELVER_DIAG_ROOT must name private evidence storage")
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(base, "work-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	return root
}

func TestFrontierDiagnosisPipeline(t *testing.T) {
	if os.Getenv("DELVER_FRONTIER_DIAG") != "1" {
		t.Skip("opt-in diagnostic: set DELVER_FRONTIER_DIAG=1 and DELVER_DIAG_ROOT")
	}
	seconds := 30 * time.Second
	if v := os.Getenv("DELVER_DIAG_DURATION"); v != "" {
		var err error
		seconds, err = time.ParseDuration(v)
		if err != nil || seconds < time.Second {
			t.Fatal("invalid DELVER_DIAG_DURATION")
		}
	}
	for _, variant := range []string{"baseline", "publisher_only", "skip_full", "publisher_only_skip_full", "capacity_bounded", "publish_first", "baseline_registry", "capacity_bounded_registry", "baseline_no_gate"} {
		t.Run(variant, func(t *testing.T) {
			const capacity = 256
			const staged = 20000
			const links = 16
			times := &diagnosisTimes{values: map[string][]float64{}}
			publisherOnly := strings.Contains(variant, "publisher_only")
			skipFull := strings.Contains(variant, "skip_full")
			boundCapacity := strings.Contains(variant, "capacity_bounded")
			var hits int64
			site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt64(&hits, 1)
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprintf(w, "<title>%s</title>", r.URL.String())
				for j := 0; j < links; j++ {
					fmt.Fprintf(w, `<a href="http://child-%d.test/%s/%d">child</a>`, j, r.URL.Host+r.URL.Path, j)
				}
			}))
			defer site.Close()
			root := diagnosisWorkDir(t)
			d := DefaultCrawlDefinition()
			d.Name = "frontier diagnostic"
			d.Scope = "open_web"
			d.Strategy = "breadth_first"
			d.RequestDelay = "0s"
			d.Termination.WhenDrained = true
			d.Proxy = ProxySettings{Mode: "http", URL: site.URL}
			no := false
			d.Network.RespectRobots = &no
			zero := 0
			d.Network.MaxRetries = &zero
			d.Fetch.Mode = "fixed"
			d.Fetch.Concurrency = 32
			d.Fetch.Maximum = 32
			d.Fetch.SampleInterval = flow.Duration(100 * time.Millisecond)
			if v := os.Getenv("DELVER_DIAG_CONCURRENCY"); v != "" {
				n, err := strconv.Atoi(v)
				if err != nil || n < 1 {
					t.Fatal("invalid DELVER_DIAG_CONCURRENCY")
				}
				d.Fetch.Concurrency = n
				d.Fetch.Maximum = n
			}
			if v := os.Getenv("DELVER_DIAG_SAMPLE"); v != "" {
				n, err := time.ParseDuration(v)
				if err != nil || n <= 0 {
					t.Fatal("invalid DELVER_DIAG_SAMPLE")
				}
				d.Fetch.SampleInterval = flow.Duration(n)
			}
			d.Fetch.DownstreamHigh = 128
			d.Fetch.DownstreamLow = 64
			if variant == "baseline_no_gate" {
				d.Fetch.DownstreamHigh = 1000000
				d.Fetch.DownstreamLow = 500000
			}
			d.Seeds = []string{"http://seed.test/extra"}
			app, err := CompileCrawl(root, "diagnosis", d)
			if err != nil {
				t.Fatal(err)
			}
			var actual *frontier.Store
			registryMutex.RLock()
			originalResource := resourceFactories["frontier_store"]
			originalWorkers := map[string]RuntimeWorkerFactory{}
			for _, kind := range []string{"frontier_admission", "frontier_accumulator", "frontier_publisher"} {
				originalWorkers[kind] = workerFactories[kind]
			}
			registryMutex.RUnlock()
			RegisterResourceWithRuntime("frontier_store", func(ctx context.Context, raw json.RawMessage, deps RuntimeDependencies) (interface{}, error) {
				var p frontier.StoreParams
				if err := json.Unmarshal(raw, &p); err != nil {
					return nil, err
				}
				p.MaxPending = capacity
				p.MaxPendingPerOrigin = capacity
				data, _ := json.Marshal(p)
				resource, err := originalResource(ctx, data, deps)
				if err != nil {
					return nil, err
				}
				actual = resource.(*frontier.Store)
				for i := 0; i < capacity; i++ {
					if err := actual.Admit(ctx, message.FetcherRequest{URI: fmt.Sprintf("http://initial-%d.test/page", i)}); err != nil {
						return nil, err
					}
				}
				requests := make([]message.FetcherRequest, staged)
				for i := range requests {
					requests[i] = message.FetcherRequest{URI: fmt.Sprintf("http://staged-%d.test/page", i)}
				}
				if err := actual.StageDiscovery(ctx, "preloaded", requests); err != nil {
					return nil, err
				}
				return actual, nil
			})
			for kind, original := range originalWorkers {
				kind, original := kind, original
				RegisterWorkerWithRuntime(kind, func(ctx context.Context, raw json.RawMessage, resources map[string]interface{}, deps RuntimeDependencies) (worker.Worker, error) {
					caller := map[string]string{"frontier_admission": "admit", "frontier_accumulator": "discover", "frontier_publisher": "publish"}[kind]
					copied := map[string]interface{}{}
					for k, v := range resources {
						copied[k] = v
					}
					wrapped := &diagnosisStore{FrontierStore: actual, ctx: ctx, caller: caller, times: times, store: actual, publisherOnly: publisherOnly, skipFull: skipFull, boundCapacity: boundCapacity, capacity: capacity}
					copied["frontier"] = wrapped
					if caller == "publish" {
						copied["requests"] = &diagnosisQueue{Queue: resources["requests"].(queue.Queue), times: times}
					}
					w, err := original(ctx, raw, copied, deps)
					if err == nil && variant == "publish_first" && caller == "publish" {
						w = &diagnosisPublishFirst{Worker: w.(*frontierworker.Worker), store: wrapped}
					}
					if err != nil {
						return nil, err
					}
					return &diagnosisWorker{Worker: w, times: times, caller: caller}, nil
				})
			}
			defer func() {
				RegisterResourceWithRuntime("frontier_store", originalResource)
				for kind, original := range originalWorkers {
					RegisterWorkerWithRuntime(kind, original)
				}
			}()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s, err := NewSupervisor(ctx, app)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err = s.Execute(ctx, "start", "", ""); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(10 * time.Second)
			for {
				s.control.mu.Lock()
				ready := s.control.prepared != nil
				s.control.mu.Unlock()
				if ready {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("runtime not ready")
				}
				time.Sleep(10 * time.Millisecond)
			}
			started := time.Now()
			beforeIO, _ := os.ReadFile("/proc/self/io")
			var checkpointStop chan struct{}
			var checkpointDone chan struct{}
			var registryBytes int
			if strings.HasSuffix(variant, "_registry") {
				checkpointStop = make(chan struct{})
				checkpointDone = make(chan struct{})
				record := &CrawlRecord{ID: "synthetic", Definition: DefaultCrawlDefinition()}
				for j := 0; j < 16000; j++ {
					record.Definition.Seeds = append(record.Definition.Seeds, fmt.Sprintf("http://registry-%d.test/%s", j, strings.Repeat("x", 600)))
				}
				w := &Workspace{root: filepath.Join(root, "registry"), records: map[string]*CrawlRecord{"synthetic": record}}
				if err := os.MkdirAll(w.root, 0700); err != nil {
					t.Fatal(err)
				}
				data, _ := json.MarshalIndent(workspaceRegistry{Version: 2, Crawls: w.records}, "", "  ")
				registryBytes = len(data)
				go func() {
					defer close(checkpointDone)
					ticker := time.NewTicker(time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-checkpointStop:
							return
						case <-ticker.C:
							times.measure(ctx, "workspace.save", func() error { return w.saveLocked() })
						}
					}
				}()
			}
			var samples []map[string]interface{}
			for time.Since(started) < seconds {
				snap := s.control.snapshot()
				samples = append(samples, map[string]interface{}{"seconds": time.Since(started).Seconds(), "snapshot": snap, "http_hits": atomic.LoadInt64(&hits)})
				time.Sleep(250 * time.Millisecond)
			}
			final := s.control.snapshot()
			elapsed := time.Since(started)
			gateSeen := false
			for _, sample := range samples {
				snapshot := sample["snapshot"].(RuntimeSnapshot)
				if snapshot.Fetch != nil && snapshot.Fetch.Reason == "downstream_high" && snapshot.Fetch.NetworkActive == 0 {
					gateSeen = true
				}
			}
			if checkpointStop != nil {
				close(checkpointStop)
				<-checkpointDone
			}
			closeStart := time.Now()
			err = s.Close()
			closeDuration := time.Since(closeStart)
			afterIO, _ := os.ReadFile("/proc/self/io")
			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)
			diagnosisWrite(t, variant, map[string]interface{}{"variant": variant, "storage_root": root, "elapsed_seconds": elapsed.Seconds(), "capacity": capacity, "initial_staged": staged, "links_per_page": links, "registry_bytes": registryBytes, "samples": samples, "final": final, "phase_times": times.snapshot(), "http_hits": atomic.LoadInt64(&hits), "close_seconds": closeDuration.Seconds(), "io_before": string(beforeIO), "io_after": string(afterIO), "heap_bytes": mem.HeapAlloc, "goroutines": runtime.NumGoroutine(), "measurement_note": "Phase times are inclusive and overlap across workers. Setup excluded; shutdown timing reported separately. Experiments alter only the test wrapper."})
			if err != nil {
				t.Fatal(err)
			}
			if os.Getenv("DELVER_DIAG_REQUIRE_GATE") == "1" && !gateSeen {
				t.Fatal("did not reproduce idle network with downstream gate")
			}
		})
	}
}

func TestFrontierDiagnosisReplay(t *testing.T) {
	path := os.Getenv("DELVER_DIAG_REPLAY")
	if path == "" {
		t.Skip("set DELVER_DIAG_REPLAY to a private disposable frontier copy")
	}
	data, err := os.ReadFile(os.Getenv("DELVER_DIAG_DEFINITION"))
	if err != nil {
		t.Fatal(err)
	}
	var d CrawlDefinition
	if err = json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	app, err := CompileCrawl(t.TempDir(), "offline-diagnostic", d)
	if err != nil {
		t.Fatal(err)
	}
	var p frontier.StoreParams
	for _, r := range app.Resources {
		if r.Type == "frontier_store" {
			if err = json.Unmarshal(r.Parameters, &p); err != nil {
				t.Fatal(err)
			}
		}
	}
	p.Path = path
	store, err := frontier.OpenStore(p)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	controller, err := flow.New(context.Background(), "replay", *d.Fetch, func() flow.Pressure { return flow.Pressure{} })
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	store.SetFlow(controller)
	before, err := store.Counts()
	if err != nil {
		t.Fatal(err)
	}
	var results []map[string]interface{}
	for _, limit := range []int{1, 128, 1024, 4096} {
		start := time.Now()
		metricsBefore := controller.Snapshot()
		countsBefore, _ := store.Counts()
		n, err := store.DrainDiscovery(context.Background(), limit)
		if err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(start)
		metricsAfter := controller.Snapshot()
		countsAfter, _ := store.Counts()
		results = append(results, map[string]interface{}{"limit": limit, "seconds": elapsed.Seconds(), "admitted": n, "storage_operations": metricsAfter.Counters["storage_operations"] - metricsBefore.Counters["storage_operations"], "capacity_deferrals": countsAfter["capacity_deferrals"] - countsBefore["capacity_deferrals"]})
	}
	after, _ := store.Counts()
	diagnosisWrite(t, "stopped-backlog-replay", map[string]interface{}{"before": before, "after": after, "drain_passes": results, "network_requests": 0})
}

// Separates global-full, one-free-slot, and host-full behavior without HTTP,
// extraction, worker contention, workspace snapshots, or a controller gate.
func TestFrontierDiagnosisCapacity(t *testing.T) {
	if os.Getenv("DELVER_FRONTIER_DIAG") != "1" {
		t.Skip("set DELVER_FRONTIER_DIAG=1 and DELVER_DIAG_ROOT")
	}
	for _, kind := range []string{"global", "host"} {
		t.Run(kind, func(t *testing.T) {
			root := diagnosisWorkDir(t)
			p := frontier.StoreParams{Path: filepath.Join(root, "frontier"), MaxPending: 1, MaxPendingPerOrigin: 1}
			if kind == "host" {
				p.MaxPending = 10000
			}
			s, err := frontier.OpenStore(p)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			policy := flow.Default()
			c, err := flow.New(context.Background(), "capacity", policy, func() flow.Pressure { return flow.Pressure{} })
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			s.SetFlow(c)
			ctx := context.Background()
			if err = s.Admit(ctx, message.FetcherRequest{URI: "http://held.test/initial"}); err != nil {
				t.Fatal(err)
			}
			requests := make([]message.FetcherRequest, 4096)
			for i := range requests {
				uri := fmt.Sprintf("http://new-%d.test/page", i)
				if kind == "host" {
					uri = fmt.Sprintf("http://held.test/new/%d", i)
				}
				requests[i] = message.FetcherRequest{URI: uri}
			}
			if err = s.StageDiscovery(ctx, "capacity-diagnostic", requests); err != nil {
				t.Fatal(err)
			}
			var passes []map[string]interface{}
			pass := func(name string, limit int) {
				before, _ := s.Counts()
				ops := c.Snapshot().Counters["storage_operations"]
				started := time.Now()
				n, err := s.DrainDiscovery(ctx, limit)
				if err != nil {
					t.Fatal(err)
				}
				elapsed := time.Since(started)
				after, _ := s.Counts()
				passes = append(passes, map[string]interface{}{"name": name, "limit": limit, "seconds": elapsed.Seconds(), "reported_admitted": n, "new_url_records": after["admitted"] - before["admitted"], "deferrals": after["capacity_deferrals"] - before["capacity_deferrals"], "storage_operations": c.Snapshot().Counters["storage_operations"] - ops, "pending": after["pending_and_active"]})
			}
			pass("blocked", 4096)
			if kind == "global" {
				free := func() {
					rows, _, err := s.Pending(ctx, "", 1)
					if err != nil || len(rows) != 1 {
						t.Fatalf("pending: %v %v", rows, err)
					}
					if err = s.Outcome(message.FetcherResponse{FetcherRequest: rows[0].Request, Success: true, HTTPCode: 200}); err != nil {
						t.Fatal(err)
					}
				}
				free()
				pass("one_free_slot_full_window", 4096)
				free()
				pass("one_free_slot_one_entry", 1)
			}
			counts, _ := s.Counts()
			if kind == "global" && (counts["admitted"] != 3 || counts["deferred_discoveries"] != 4094 || counts["pending_and_active"] != 1) {
				t.Fatalf("unexpected counts %v", counts)
			}
			if kind == "host" && (counts["admitted"] != 1 || counts["deferred_discoveries"] != 4096 || counts["pending_and_active"] != 1) {
				t.Fatalf("unexpected counts %v", counts)
			}
			diagnosisWrite(t, "capacity-"+kind, map[string]interface{}{"passes": passes, "final": counts, "network_requests": 0})
		})
	}
}

// Replay real saved analysis messages against the disposable frontier copy.
// No fetch workers or network clients are started.
func TestFrontierDiagnosisAnalysisReplay(t *testing.T) {
	path := os.Getenv("DELVER_DIAG_REPLAY")
	if path == "" {
		t.Skip("set DELVER_DIAG_REPLAY, DELVER_DIAG_DEFINITION, DELVER_DIAG_MESSAGES, DELVER_DIAG_ROOT")
	}
	data, err := os.ReadFile(os.Getenv("DELVER_DIAG_DEFINITION"))
	if err != nil {
		t.Fatal(err)
	}
	var d CrawlDefinition
	if err = json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	app, err := CompileCrawl(t.TempDir(), "analysis-replay", d)
	if err != nil {
		t.Fatal(err)
	}
	var params frontier.StoreParams
	for _, r := range app.Resources {
		if r.Type == "frontier_store" {
			if err = json.Unmarshal(r.Parameters, &params); err != nil {
				t.Fatal(err)
			}
		}
	}
	params.Path = path
	s, err := frontier.OpenStore(params)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, err := flow.New(context.Background(), "analysis-replay", *d.Fetch, func() flow.Pressure { return flow.Pressure{} })
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s.SetFlow(c)
	files, err := os.ReadDir(os.Getenv("DELVER_DIAG_MESSAGES"))
	if err != nil {
		t.Fatal(err)
	}
	type analysis struct {
		msg   types.Message
		links int
		bytes int
	}
	var messages []analysis
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(os.Getenv("DELVER_DIAG_MESSAGES"), file.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var msg types.Message
		if err = json.Unmarshal(raw, &msg); err != nil {
			t.Fatal(err)
		}
		var body struct {
			Features map[string]json.RawMessage `json:"features"`
		}
		if err = json.Unmarshal(msg.Message, &body); err != nil {
			t.Fatal(err)
		}
		var links []string
		if raw := body.Features["url"]; len(raw) > 0 {
			if err = json.Unmarshal(raw, &links); err != nil {
				t.Fatal(err)
			}
		}
		messages = append(messages, analysis{msg: msg, links: len(links), bytes: len(raw)})
	}
	if len(messages) < 7 {
		t.Fatal("at least seven saved analysis messages required")
	}
	sort.SliceStable(messages, func(i, j int) bool { return messages[i].links < messages[j].links })
	var outputs []map[string]interface{}
	process := func(label string, selected []analysis) {
		times := &diagnosisTimes{values: map[string][]float64{}}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		wrapped := &diagnosisStore{FrontierStore: s, ctx: ctx, caller: label, times: times, store: s}
		w, err := frontierworker.New(frontierworker.Params{Store: wrapped, Scope: d.Scope, MaxDepth: d.MaxDepth, MaxHops: d.MaxHops, AllowPattern: d.AllowPattern, DenyPattern: d.DenyPattern, AllowedHostSuffixes: d.Network.AllowedHostSuffixes}, "accumulator")
		if err != nil {
			t.Fatal(err)
		}
		w.SetFlow(c, *d.Fetch)
		worker := &diagnosisWorker{Worker: w, times: times, caller: label}
		var wg sync.WaitGroup
		errs := make(chan error, len(selected))
		counts := make([]int, len(selected))
		sizes := make([]int, len(selected))
		before, _ := s.Counts()
		started := time.Now()
		for i, m := range selected {
			counts[i] = m.links
			sizes[i] = m.bytes
			wg.Add(1)
			go func(m analysis) { defer wg.Done(); _, err := worker.OnMessageContext(ctx, m.msg); errs <- err }(m)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		after, _ := s.Counts()
		outputs = append(outputs, map[string]interface{}{"label": label, "seconds": time.Since(started).Seconds(), "links": counts, "message_bytes": sizes, "phase_times": times.snapshot(), "before": before, "after": after})
	}
	process("zero_links", messages[:1])
	process("median_links", messages[len(messages)/2:len(messages)/2+1])
	process("largest_message", messages[len(messages)-1:])
	process("four_workers", messages[len(messages)/2+1:len(messages)/2+5])
	diagnosisWrite(t, "actual-analysis-replay", map[string]interface{}{"message_count": len(messages), "experiments": outputs, "network_requests": 0})
}

// Opt-in recovery check on disposable copies only. Prepare never starts HTTP.
func TestFrontierDiagnosisIdentityRecovery(t *testing.T) {
	path := os.Getenv("DELVER_DIAG_REPLAY")
	if path == "" {
		t.Skip("set private DELVER_DIAG_REPLAY, DELVER_DIAG_QUEUE, DELVER_DIAG_CRAWL_ID, DELVER_DIAG_EXPECT_REPAIRS and DELVER_DIAG_ROOT")
	}
	expected, err := strconv.Atoi(os.Getenv("DELVER_DIAG_EXPECT_REPAIRS"))
	if err != nil || expected < 1 {
		t.Fatal("positive DELVER_DIAG_EXPECT_REPAIRS required")
	}
	s, err := frontier.OpenStore(frontier.StoreParams{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	q, err := queue.NewCrawlQueue(queue.CrawlQueueParams{Path: os.Getenv("DELVER_DIAG_QUEUE"), CrawlID: os.Getenv("DELVER_DIAG_CRAWL_ID"), MaxAttempts: 3, AcceptFrontierGenerations: true})
	if err != nil {
		t.Fatal(err)
	}
	defer q.(interface{ Close() error }).Close()
	counts := q.(interface {
		Counts() (map[string]int64, error)
	})
	before, err := counts.Counts()
	if err != nil {
		t.Fatal(err)
	}
	if before["completed"] == 0 {
		t.Fatal("disposable copy must contain completed crawl records")
	}
	q.(interface {
		SetFrontierIdentityLookup(func(string) (message.FetcherRequest, bool, error))
	}).SetFrontierIdentityLookup(s.DispatchedRequest)
	if err = q.Prepare(); err != nil {
		t.Fatal(err)
	}
	after, err := counts.Counts()
	if err != nil {
		t.Fatal(err)
	}
	if after["pending"]-before["pending"] != int64(expected) {
		t.Fatalf("unexpected repaired count before=%v after=%v expected=%d", before, after, expected)
	}
	if err = q.Prepare(); err != nil {
		t.Fatal(err)
	}
	again, err := counts.Counts()
	if err != nil {
		t.Fatal(err)
	}
	if again["pending"] != after["pending"] || again["completed"] != after["completed"] {
		t.Fatal("identity repair was not idempotent")
	}
	diagnosisWrite(t, "identity-recovery", map[string]interface{}{"before": before, "after": after, "repaired": expected, "idempotent": true, "network_requests": 0})
}
