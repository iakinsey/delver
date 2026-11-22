package application

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/worker"
	"github.com/stretchr/testify/require"
)

type soakPressureResource struct{ flag, purgeFailure *int32 }

var _ worker.HealthSource = soakPressureResource{}

func (r soakPressureResource) Health() string {
	if atomic.LoadInt32(r.flag) != 0 {
		return "injected soak storage pressure"
	}
	return ""
}

func (r soakPressureResource) SetProtection(*protection.Policy) {}
func (r soakPressureResource) PurgeMatching(context.Context, func(string) bool) error {
	if r.purgeFailure != nil && atomic.LoadInt32(r.purgeFailure) != 0 {
		return fmt.Errorf("injected soak purge failure")
	}
	return nil
}
func (r soakPressureResource) VerifyNoMatches(context.Context, func(string) bool) error { return nil }

// Opt-in real-clock, full worker-pipeline endurance test. Every destination is
// served by an isolated local HTTP proxy; it never contacts the public network.
func TestWorkspaceSoak(t *testing.T) {
	if os.Getenv("DELVER_SOAK") != "1" {
		t.Skip("set DELVER_SOAK=1; default duration 1h")
	}
	duration := time.Hour
	if raw := os.Getenv("DELVER_SOAK_DURATION"); raw != "" {
		var err error
		duration, err = time.ParseDuration(raw)
		require.NoError(t, err)
		require.GreaterOrEqual(t, duration, 30*time.Second)
	}
	base := os.Getenv("DELVER_VALIDATION_ROOT")
	if base == "" {
		base = t.TempDir()
	}
	require.NoError(t, os.MkdirAll(base, 0700))
	root, err := os.MkdirTemp(base, "crawl-soak-")
	require.NoError(t, err)
	defer func() {
		if !t.Failed() {
			require.NoError(t, os.RemoveAll(root))
		} else {
			t.Logf("retained failing soak storage: %s", root)
		}
	}()
	report, err := os.Create(filepath.Join(base, "soak-samples.jsonl"))
	require.NoError(t, err)
	defer report.Close()
	emit := func(v interface{}) {
		raw, e := json.Marshal(v)
		require.NoError(t, e)
		_, e = fmt.Fprintln(report, string(raw))
		require.NoError(t, e)
		require.NoError(t, report.Sync())
	}
	baselineGoroutines := runtime.NumGoroutine()
	var hits, badUA, robots, timeouts, overloads, truncated, protectedSiteHits int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&hits, 1)
		if strings.HasSuffix(r.URL.Hostname(), ".soak-7.invalid") {
			atomic.AddInt64(&protectedSiteHits, 1)
		}
		if r.UserAgent() != "DelverSoak/1.0" {
			atomic.AddInt64(&badUA, 1)
		}
		if r.URL.Path == "/robots.txt" {
			atomic.AddInt64(&robots, 1)
		}
		if n%97 == 0 {
			atomic.AddInt64(&timeouts, 1)
			<-r.Context().Done()
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(80 * time.Millisecond):
		}
		if n%43 == 0 {
			atomic.AddInt64(&overloads, 1)
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		if n%29 == 0 {
			atomic.AddInt64(&overloads, 1)
			w.WriteHeader(503)
			return
		}
		if n%71 == 0 {
			atomic.AddInt64(&truncated, 1)
			w.Header().Set("Content-Length", "10000")
			fmt.Fprint(w, "truncated")
			return
		}
		if n%53 == 0 {
			w.WriteHeader(404)
			return
		}
		page, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/page/"))
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><title>soak %d</title><a href="/page/%d">next</a><a href="/page/%d">other</a>%s</html>`, n, (page+1)%64, (page+7)%64, strings.Repeat("bounded fixture payload ", 24))
	}))
	defer proxy.Close()
	options := Options{DataDirectory: root}
	workspace, err := NewWorkspace(context.Background(), options)
	require.NoError(t, err)
	defer func() {
		if workspace != nil {
			require.NoError(t, workspace.Close())
		}
	}()
	// Capture live stacks before closing resources when an endurance assertion
	// fails; shutdown otherwise removes the evidence needed to diagnose a stall.
	defer func() {
		if !t.Failed() {
			return
		}
		if f, e := os.Create(filepath.Join(root, "failure-goroutines.txt")); e == nil {
			_ = pprof.Lookup("goroutine").WriteTo(f, 2)
			_ = f.Close()
		}
	}()
	_, err = workspace.HandleWorkspace(context.Background(), "settings", json.RawMessage(`{"revision":1,"execution":{"concurrency":10,"host_concurrency":2,"host_delay":"0s"}}`))
	require.NoError(t, err)
	purgeFailure := new(int32)
	pressureFlags := map[string]*int32{"fixed": new(int32), "adaptive": new(int32)}
	RegisterResource("soak_pressure_fixture", func(raw json.RawMessage) (interface{}, error) {
		var p struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		flag := pressureFlags[p.Mode]
		if flag == nil {
			return nil, fmt.Errorf("invalid soak pressure mode")
		}
		return soakPressureResource{flag: flag, purgeFailure: purgeFailure}, nil
	}, ProtectionSupport{Configuration: func(raw json.RawMessage, _ func(string) bool) (json.RawMessage, error) { return raw, nil }})
	defer RegisterResource("soak_pressure_fixture", nil)
	ids := []string{}
	runIDs := map[string]string{}
	previous := map[string]int64{}
	for _, mode := range []string{"fixed", "adaptive"} {
		d := DefaultCrawlDefinition()
		d.Name = "soak " + mode
		d.Scope = "open_web"
		d.RequestDelay = "0s"
		d.Revisit = "4s"
		d.MaxDepth = 1000
		for host := 0; host < 8; host++ {
			d.Seeds = append(d.Seeds, fmt.Sprintf("http://%s-%d.soak-%d.invalid/page/0", mode, host, host))
		}
		d.Proxy = ProxySettings{Mode: "http", URL: proxy.URL}
		no := false
		d.Network.RespectRobots = &no
		d.Network.UserAgent = "DelverSoak/1.0"
		d.Network.RequestTimeout = "250ms"
		d.Network.RequireProxy = true
		d.Network.RetryDelay = "50ms"
		d.Network.RetryMaxDelay = "200ms"
		d.Retention = frontier.Retention{CompletedFor: frontier.Lifetime(2 * time.Second), FailedFor: frontier.Lifetime(2 * time.Second), DedupFor: frontier.Lifetime(3 * time.Second), CleanupInterval: frontier.Lifetime(time.Second), CleanupBatchSize: 512}
		p := d.Fetch
		p.Mode = mode
		p.Concurrency = 4
		p.Minimum = 2
		p.Maximum = 8
		p.SampleInterval = flow.Duration(200 * time.Millisecond)
		p.AdjustmentInterval = flow.Duration(2 * time.Second)
		p.MinSamples = 5
		p.HostCooldown = flow.Duration(500 * time.Millisecond)
		p.HostCooldownMax = flow.Duration(2 * time.Second)
		p.RecoveryProbeInterval = flow.Duration(500 * time.Millisecond)
		p.FailureThreshold = 3
		p.MetricsSamples = 128
		p.MetricsSaveInterval = flow.Duration(time.Second)
		p.DownstreamHigh, p.DownstreamLow = 8, 4
		record := createWorkspaceCrawl(t, workspace, d)
		pipeline, e := CompileCrawl(filepath.Join(root, "fixture-pipeline", mode), "fixture", d)
		require.NoError(t, e)
		for i := range pipeline.Resources {
			if pipeline.Resources[i].Type == "frontier_store" {
				var limits frontier.StoreParams
				require.NoError(t, json.Unmarshal(pipeline.Resources[i].Parameters, &limits))
				limits.MaxPending, limits.MaxPendingPerOrigin = 12, 2
				pipeline.Resources[i].Parameters, e = json.Marshal(limits)
				require.NoError(t, e)
			}
		}
		parameters, _ := json.Marshal(map[string]string{"mode": mode})
		pipeline.Resources = append(pipeline.Resources, config.Resource{Name: "soak-pressure", Type: "soak_pressure_fixture", Parameters: parameters})
		draft := workspaceCall(t, workspace, "draft", map[string]interface{}{"crawl_id": record["id"], "revision": record["revision"], "configuration": pipeline}).(map[string]interface{})
		workspaceCall(t, workspace, "activate", map[string]interface{}{"crawl_id": record["id"], "revision": draft["revision"]})
		waitWorkspace(t, workspace, record["id"].(string), func(v map[string]interface{}) bool { return runtimeState(v) == "running" })
		id := record["id"].(string)
		ids = append(ids, id)
		detail := workspaceCall(t, workspace, "detail", map[string]string{"crawl_id": id}).(map[string]interface{})
		runIDs[id] = latestRun(detail)["id"].(string)
	}
	controllersForRuns := func() map[string]*flow.Controller {
		controllers := map[string]*flow.Controller{}
		for _, id := range ids {
			workspace.mu.Lock()
			slot := workspace.slots[id]
			workspace.mu.Unlock()
			slot.runtimeMu.RLock()
			supervisor := slot.supervisor
			slot.runtimeMu.RUnlock()
			supervisor.mu.Lock()
			control := supervisor.control
			supervisor.mu.Unlock()
			require.NotNil(t, control)
			control.mu.Lock()
			prepared := control.prepared
			control.mu.Unlock()
			require.NotNil(t, prepared)
			controllers[id] = prepared.flow
		}
		return controllers
	}
	closeAndVerify := func() {
		controllers := controllersForRuns()
		require.NoError(t, workspace.Close())
		for id, controller := range controllers {
			raw, e := os.ReadFile(filepath.Join(workspace.runRoot(id, runIDs[id]), "state.json.fetch.json"))
			require.NoError(t, e)
			var saved struct {
				Totals flow.Totals `json:"totals"`
			}
			require.NoError(t, json.Unmarshal(raw, &saved))
			require.Equal(t, controller.Snapshot().Totals, saved.Totals, "shutdown lost completed-attempt metrics")
			previous[id] = saved.Totals.Attempts
		}
	}
	started := time.Now()
	purgeDone := false
	// Physical cleanup scans accumulated results under the race detector. The
	// retained ten-minute fixture takes about 47 seconds to rewrite and verify;
	// this deadline is separate from immediate blocking and request cancellation.
	const purgeDeadline = 2 * time.Minute
	nextRestart := duration / 4
	nextPressure := duration / 8
	pressurePulses := 0
	restarts := 0
	samples := 0
	var maximumHeap uint64
	peakGoroutines := 0
	var lastRequests int64
	capacityPeaks := map[string]int64{}
	hostDeferrals := map[string]int64{}
	for time.Since(started) < duration {
		if !purgeDone && time.Since(started) >= duration/6 {
			atomic.StoreInt32(purgeFailure, 1)
			incident, e := workspace.protection.Trigger("http://fixed-7.soak-7.invalid/page/0", "synthetic-soak", ids[0], runIDs[ids[0]], 0)
			require.ErrorIs(t, e, protection.ErrBlocked)
			require.Eventually(t, func() bool {
				i, e := workspace.protection.PrivateIncident(incident.ID)
				return e == nil && i.State == "failed"
			}, purgeDeadline, 20*time.Millisecond)
			failed, e := workspace.protection.PrivateIncident(incident.ID)
			require.NoError(t, e)
			require.Contains(t, failed.Error, "injected soak purge failure")
			atomic.StoreInt32(purgeFailure, 0)
			retry, _ := json.Marshal(map[string]string{"action": "retry", "id": incident.ID})
			_, e = workspace.HandleWorkspace(context.Background(), "protection", retry)
			require.NoError(t, e)
			require.Eventually(t, func() bool {
				i, e := workspace.protection.PrivateIncident(incident.ID)
				return e == nil && (i.State == "completed" || i.State == "failed")
			}, purgeDeadline, 20*time.Millisecond)
			completed, e := workspace.protection.PrivateIncident(incident.ID)
			require.NoError(t, e)
			require.Equal(t, "completed", completed.State, completed.Error)
			require.Equal(t, failed.Targets, completed.Targets)
			requests := atomic.LoadInt64(&protectedSiteHits)
			time.Sleep(300 * time.Millisecond)
			require.Equal(t, requests, atomic.LoadInt64(&protectedSiteHits), "purged site continued receiving requests")
			registry, e := os.ReadFile(filepath.Join(root, "crawls.json"))
			require.NoError(t, e)
			require.NotContains(t, string(registry), "soak-7.invalid")
			purgeDone = true
			t.Logf("SOAK protection incident=%s failed visibly and retried; shared site removed across both crawls", incident.ID)
		}

		if time.Since(started) >= nextRestart && restarts < 3 {
			closeAndVerify()
			workspace = nil
			workspace, err = NewWorkspace(context.Background(), options)
			require.NoError(t, err)
			for _, id := range ids {
				workspaceCall(t, workspace, "command", map[string]string{"crawl_id": id, "action": "resume", "id": fmt.Sprintf("soak-restart-%d-%s", restarts, id)})
				waitWorkspace(t, workspace, id, func(v map[string]interface{}) bool { return runtimeState(v) == "running" })
			}
			restarts++
			nextRestart = duration * time.Duration(restarts+1) / 4
		}
		if time.Since(started) >= nextPressure {
			controllers := controllersForRuns()
			effective := map[string]int{}
			for _, flag := range pressureFlags {
				atomic.StoreInt32(flag, 1)
			}
			for _, c := range controllers {
				require.Eventually(t, func() bool {
					return strings.HasPrefix(c.Snapshot().Reason, "storage_health:") && c.Snapshot().Active == 0
				}, 3*time.Second, 20*time.Millisecond)
			}
			attempts := map[string]int64{}
			for id, c := range controllers {
				attempts[id] = c.Snapshot().Totals.Attempts
				effective[id] = c.Effective()
			}
			time.Sleep(250 * time.Millisecond)
			for id, c := range controllers {
				require.Equal(t, effective[id], c.Effective(), "admission blocking shrank concurrency")
				require.Equal(t, attempts[id], c.Snapshot().Totals.Attempts, "fetches continued through storage-health admission block")
			}
			// Durable pending work must not prevent pause while admission is blocked.
			for _, id := range ids {
				workspaceCall(t, workspace, "command", map[string]string{"crawl_id": id, "action": "pause", "id": fmt.Sprintf("soak-pressure-pause-%d-%s", pressurePulses, id)})
				waitWorkspace(t, workspace, id, func(v map[string]interface{}) bool { return runtimeState(v) == "paused" })
				require.Zero(t, controllers[id].Snapshot().Active)
				workspaceCall(t, workspace, "command", map[string]string{"crawl_id": id, "action": "resume", "id": fmt.Sprintf("soak-pressure-resume-%d-%s", pressurePulses, id)})
				waitWorkspace(t, workspace, id, func(v map[string]interface{}) bool { return runtimeState(v) == "running" })
			}
			for _, flag := range pressureFlags {
				atomic.StoreInt32(flag, 0)
			}
			for id, c := range controllers {
				require.Eventually(t, func() bool { return c.Snapshot().Totals.Attempts > attempts[id] }, 8*time.Second, 20*time.Millisecond, "pressure did not recover in same run")
			}
			pressurePulses++
			nextPressure += duration / 8
			t.Logf("SOAK pressure pulse=%d blocked and recovered", pressurePulses)
		}
		row := map[string]interface{}{"elapsed_seconds": time.Since(started).Seconds(), "restarts": restarts, "requests": atomic.LoadInt64(&hits)}
		for _, id := range ids {
			detail := workspaceCall(t, workspace, "detail", map[string]string{"crawl_id": id}).(map[string]interface{})
			require.Equal(t, "running", runtimeState(detail), "%v", detail)
			require.Equal(t, runIDs[id], latestRun(detail)["id"], "resume must retain run identity")
			outer := detail["runtime"].(map[string]interface{})
			frame := outer["runtime"].(map[string]interface{})
			fetch := frame["fetch"].(map[string]interface{})
			totals := fetch["totals"].(map[string]interface{})
			attempts := int64(totals["attempts"].(float64))
			require.GreaterOrEqual(t, attempts, previous[id], "metrics lost across restart")
			previous[id] = attempts
			require.LessOrEqual(t, fetch["process_active"].(float64), float64(10))
			require.GreaterOrEqual(t, fetch["effective"].(float64), float64(2))
			require.LessOrEqual(t, fetch["effective"].(float64), float64(8))
			require.Empty(t, frame["health"], "pipeline health failed: %v", frame["health"])
			front := frame["frontier"].(map[string]interface{})
			pending := int64(front["pending_and_active"].(float64))
			require.LessOrEqual(t, pending, int64(12), "soak exceeded frontier capacity")
			if pending > capacityPeaks[id] {
				capacityPeaks[id] = pending
			}
			hostDeferrals[id] = int64(front["capacity_deferrals"].(float64))
			if samples > 0 && front["terminal_outcomes"].(float64) > 10 {
				require.Greater(t, front["cleaned"].(float64), float64(0), "configured short retention cleanup never ran")
			}
			queues := frame["queue_states"].(map[string]interface{})
			for name, raw := range queues {
				counts := raw.(map[string]interface{})
				require.Zero(t, counts["failed"], name)
			}
			require.Less(t, len(fetch["http_statuses"].(map[string]interface{})), 20)
			row[id] = map[string]interface{}{"fetch": fetch, "frontier": frame["frontier"], "queues": queues}
		}
		if samples > 0 {
			require.Greater(t, atomic.LoadInt64(&hits), lastRequests, "no network progress in sampling window")
		}
		lastRequests = atomic.LoadInt64(&hits)
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		row["heap_before_gc_bytes"] = memory.HeapAlloc
		runtime.GC()
		runtime.ReadMemStats(&memory)
		if memory.HeapAlloc > maximumHeap {
			maximumHeap = memory.HeapAlloc
		}
		require.Less(t, memory.HeapAlloc, uint64(768<<20), "live heap exceeded 768 MiB")
		goroutines := runtime.NumGoroutine()
		if goroutines > peakGoroutines {
			peakGoroutines = goroutines
		}
		require.Less(t, goroutines, 1500)
		row["heap_bytes"] = memory.HeapAlloc
		row["goroutines"] = goroutines
		emit(row)
		t.Logf("SOAK elapsed=%s restarts=%d requests=%d heap_MiB=%.1f goroutines=%d", time.Since(started).Round(time.Second), restarts, atomic.LoadInt64(&hits), float64(memory.HeapAlloc)/(1<<20), goroutines)
		samples++
		remaining := duration - time.Since(started)
		if remaining <= 0 {
			break
		}
		wait := 10 * time.Second
		if restarts < 3 {
			if until := nextRestart - time.Since(started); until < wait {
				wait = until
			}
		}
		if until := nextPressure - time.Since(started); until < wait {
			wait = until
		}
		if wait < 0 {
			wait = 0
		}
		if remaining < wait {
			wait = remaining
		}
		time.Sleep(wait)
	}
	closeAndVerify()
	workspace = nil
	require.Eventually(t, func() bool { return runtime.NumGoroutine() <= baselineGoroutines+100 }, 10*time.Second, 100*time.Millisecond)
	require.Equal(t, 3, restarts, "soak must exercise all three workspace restarts")
	require.True(t, purgeDone, "soak did not exercise purge and retry")
	require.Greater(t, pressurePulses, 0)
	require.Zero(t, atomic.LoadInt64(&badUA))
	require.Zero(t, atomic.LoadInt64(&robots))
	require.Greater(t, atomic.LoadInt64(&timeouts), int64(0))
	require.Greater(t, atomic.LoadInt64(&overloads), int64(0))
	require.Greater(t, atomic.LoadInt64(&truncated), int64(0))
	for _, id := range ids {
		require.Greater(t, previous[id], int64(100))
		require.EqualValues(t, 12, capacityPeaks[id], "soak never reached global frontier capacity")
		require.Greater(t, hostDeferrals[id], int64(0), "soak never exercised blocked-host discovery")
	}
	emit(map[string]interface{}{"complete": true, "duration_seconds": time.Since(started).Seconds(), "requests": atomic.LoadInt64(&hits), "timeouts_injected": atomic.LoadInt64(&timeouts), "overload_responses_injected": atomic.LoadInt64(&overloads), "truncated_bodies_injected": atomic.LoadInt64(&truncated), "maximum_sampled_live_heap_bytes": maximumHeap, "peak_goroutines": peakGoroutines, "restarts": restarts, "samples": samples, "pressure_pulses": pressurePulses, "protection_purge_and_retry": purgeDone, "frontier_capacity_peaks": capacityPeaks, "host_capacity_deferrals": hostDeferrals})
}
