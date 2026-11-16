//go:build go1.19
// +build go1.19

package frontier

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
)

func millionParams(path string, n int) StoreParams {
	return StoreParams{Path: path, CacheBudgetMB: 64, MaxPending: n + 1000, MaxPendingPerOrigin: n + 1000, OriginDelay: Lifetime(time.Nanosecond), Retention: Retention{CompletedFor: Lifetime(time.Second), FailedFor: Lifetime(time.Second), DedupFor: Lifetime(2 * time.Second), CleanupInterval: Lifetime(time.Second), CleanupBatchSize: 4096}, Retry: &RetryPolicy{Share: .2, Limit: 1, Base: flow.Duration(100 * time.Millisecond), Maximum: flow.Duration(time.Second), FailureThreshold: 1000, HostCooldown: flow.Duration(time.Second), HostCooldownMax: flow.Duration(time.Minute), ProbeInterval: flow.Duration(time.Millisecond)}}
}
func millionURI(i int) string {
	return fmt.Sprintf("http://host-%05d.scale.invalid/page/%08d", i%10000, i)
}
func millionCandidate(t *testing.T, s *Store, uri string) Candidate {
	t.Helper()
	var c Candidate
	require.NoError(t, s.db.View(func(tx *badger.Txn) error { return read(tx, "u/"+uri, &c) }))
	return c
}
func millionKeys(t *testing.T, s *Store, prefix string) int {
	t.Helper()
	n := 0
	require.NoError(t, s.db.View(func(tx *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := tx.NewIterator(opts)
		defer it.Close()
		for it.Seek([]byte(prefix)); it.ValidForPrefix([]byte(prefix)); it.Next() {
			n++
		}
		return nil
	}))
	return n
}
func TestFrontierMillionURLs(t *testing.T) {
	if os.Getenv("DELVER_FRONTIER_MILLION") != "1" {
		t.Skip("set DELVER_FRONTIER_MILLION=1 for real million-URL lifecycle workload")
	}
	n := 1000000
	if raw := os.Getenv("DELVER_FRONTIER_URLS"); raw != "" {
		var err error
		n, err = strconv.Atoi(raw)
		require.NoError(t, err)
		require.GreaterOrEqual(t, n, 1000)
	}
	base := os.Getenv("DELVER_VALIDATION_ROOT")
	if base == "" {
		base = t.TempDir()
	}
	require.NoError(t, os.MkdirAll(base, 0700))
	root := os.Getenv("DELVER_FRONTIER_RESUME_ROOT")
	resuming := root != ""
	var err error
	if !resuming {
		root, err = os.MkdirTemp(base, "million-frontier-")
		require.NoError(t, err)
		raw, _ := json.Marshal(map[string]interface{}{"kind": "delver-frontier-validation", "urls": n})
		require.NoError(t, os.WriteFile(filepath.Join(root, "validation-fixture.json"), raw, 0600))
	} else {
		require.True(t, filepath.IsAbs(root) && strings.HasPrefix(filepath.Base(root), "million-frontier-"), "resume must identify isolated validation storage")
		raw, e := os.ReadFile(filepath.Join(root, "validation-fixture.json"))
		require.NoError(t, e)
		var marker struct {
			Kind string `json:"kind"`
			URLs int    `json:"urls"`
		}
		require.NoError(t, json.Unmarshal(raw, &marker))
		require.Equal(t, "delver-frontier-validation", marker.Kind)
		require.Equal(t, n, marker.URLs)
	}

	defer func() {
		if !t.Failed() {
			require.NoError(t, os.RemoveAll(root))
		} else {
			t.Logf("retained failing scale storage: %s", root)
		}
	}()
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if resuming {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	report, err := os.OpenFile(filepath.Join(base, "million-frontier-samples.jsonl"), flags, 0600)
	require.NoError(t, err)
	defer report.Close()
	started := time.Now()
	var peakHeap, peakRSS uint64
	emit := func(phase string, extra map[string]interface{}) {
		runtime.GC()
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		rss := residentBytes()
		if mem.HeapAlloc > peakHeap {
			peakHeap = mem.HeapAlloc
		}
		if rss > peakRSS {
			peakRSS = rss
		}
		if mem.HeapAlloc >= 512<<20 {
			f, e := os.Create(filepath.Join(base, "million-frontier-heap.pprof"))
			require.NoError(t, e)
			require.NoError(t, pprof.WriteHeapProfile(f))
			require.NoError(t, f.Close())
		}
		require.Less(t, mem.HeapAlloc, uint64(512<<20), "scale heap above 512 MiB")
		require.Less(t, rss, uint64(2<<30), "scale RSS above 2 GiB")
		row := map[string]interface{}{"phase": phase, "elapsed_seconds": time.Since(started).Seconds(), "urls": n, "heap_bytes": mem.HeapAlloc, "rss_bytes": rss, "disk_bytes": diskUsage(root)}
		for k, v := range extra {
			row[k] = v
		}
		raw, e := json.Marshal(row)
		require.NoError(t, e)
		_, e = fmt.Fprintln(report, string(raw))
		require.NoError(t, e)
		require.NoError(t, report.Sync())
		t.Log(string(raw))
	}
	params := millionParams(filepath.Join(root, "db"), n)
	s, err := OpenStore(params)
	require.NoError(t, err)
	defer func() {
		if s != nil {
			require.NoError(t, s.Close())
		}
	}()
	ctx := context.Background()
	probe := func(phase string) {
		times := map[string]float64{}
		for _, strategy := range []string{"breadth_first", "depth_first", "host_diverse", "adaptive"} {
			durations := []time.Duration{}
			for sample := 0; sample < 20; sample++ {
				before := time.Now()
				rows, e := s.Candidates(ctx, strategy, "", 128)
				durations = append(durations, time.Since(before))
				require.NoError(t, e)
				require.Len(t, rows, 128)
				seen := map[string]bool{}
				for _, c := range rows {
					require.False(t, seen[c.Request.URI])
					seen[c.Request.URI] = true
				}
			}
			sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
			p95 := durations[18]
			require.Less(t, p95, time.Second, "bounded %s selection took over a second", strategy)
			times[strategy] = float64(p95.Microseconds()) / 1000
		}
		emit(phase, map[string]interface{}{"candidate_128_p95_ms": times})
	}
	startAt := 0
	if resuming {
		state, e := s.Counts()
		require.NoError(t, e)
		require.Zero(t, state["terminal_outcomes"])
		require.Zero(t, state["reserved"])
		require.Zero(t, state["retry_pending"])
		startAt = int(state["admitted"])
		require.Greater(t, startAt, 0)
		require.LessOrEqual(t, startAt, n)
		require.Equal(t, int64(startAt), millionCandidate(t, s, millionURI(startAt-1)).Sequence)
		emit("resumed_admission", map[string]interface{}{"previously_admitted": startAt})
	}
	batch := n / 10
	admissionStart := time.Now()
	for i := startAt; i < n; i++ {
		require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: millionURI(i)}))
		if (i+1)%batch == 0 {
			emit("admitting", map[string]interface{}{"admitted": i + 1, "admission_urls_per_second": float64(i+1-startAt) / time.Since(admissionStart).Seconds()})
		}
	}
	counts, err := s.Counts()
	require.NoError(t, err)
	require.Equal(t, int64(n), counts["admitted"])
	require.Equal(t, int64(n), counts["pending_and_active"])
	for i := 0; i < 128; i++ {
		require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: millionURI(i*n/128) + "#duplicate"}))
	}
	counts, err = s.Counts()
	require.NoError(t, err)
	require.Equal(t, int64(n), counts["admitted"])
	probe("million_active")
	// Simulate an interrupted index upgrade, then rebuild from all million active
	// requests. Normal startup thereafter must skip this completed migration.
	require.NoError(t, s.db.Update(func(tx *badger.Txn) error {
		for i := 0; i < 128; i++ {
			c := millionCandidateRead(tx, millionURI(i))
			for _, key := range schedulingKeys(c) {
				if key[0] == 'f' {
					if err := tx.Delete([]byte(key)); err != nil {
						return err
					}
				}
			}
		}
		return tx.Delete([]byte("meta/fresh_index_v1"))
	}))
	require.NoError(t, s.Close())
	s = nil
	before := time.Now()
	s, err = OpenStore(params)
	require.NoError(t, err)
	emit("million_active_index_upgrade", map[string]interface{}{"reopen_seconds": time.Since(before).Seconds()})
	probe("after_upgrade")
	// Run 90% of admitted URLs through real reservation/publication/outcome APIs.
	// Keep retry-wait records alive while building a large terminal history.
	target := n * 9 / 10
	if raw := os.Getenv("DELVER_FRONTIER_HISTORY_URLS"); raw != "" {
		target, err = strconv.Atoi(raw)
		require.NoError(t, err)
		require.GreaterOrEqual(t, target, 1000)
		require.Less(t, target, n-256)
	}
	processed := 0
	retryOriginal := map[string]Candidate{}
	for processed < target {
		rows, e := s.Candidates(ctx, "breadth_first", "", 128)
		require.NoError(t, e)
		require.NotEmpty(t, rows)
		for _, c := range rows {
			if processed >= target {
				break
			}
			c, e = s.Reserve(ctx, c)
			require.NoError(t, e)
			require.NoError(t, s.Published(c))
			response := message.FetcherResponse{FetcherRequest: c.Request, Success: true, HTTPCode: 200}
			if processed < 64 {
				response.Success = false
				response.HTTPCode = 0
				response.Retryable = true
				response.FailureClass = "timeout"
				retryOriginal[c.Request.URI] = c
			}
			require.NoError(t, s.Outcome(response))
			processed++
			if processed%batch == 0 {
				emit("building_terminal_history", map[string]interface{}{"processed": processed})
			}
		}
	}
	counts, err = s.Counts()
	require.NoError(t, err)
	require.Equal(t, int64(64), counts["retry_pending"])
	require.Equal(t, int64(target-64), counts["terminal_outcomes"])
	probe("large_terminal_history")
	rows, cursor, err := s.Pending(ctx, "", 128)
	require.NoError(t, err)
	require.Len(t, rows, 128)
	cursorSequence := rows[len(rows)-1].Sequence
	// Delete the record naming this cursor through normal outcomes. Subsequent
	// seeking must continue correctly after both its indexes and details vanish.
	for _, c := range rows {
		c, err = s.Reserve(ctx, c)
		require.NoError(t, err)
		require.NoError(t, s.Published(c))
		require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: c.Request, Success: true, HTTPCode: 200}))
	}
	terminal := target - 64 + 128
	require.NoError(t, s.Close())
	s = nil
	// The helper exits without DB.Close after synchronous admissions, exercising
	// actual process death/WAL replay rather than only an in-process reopen.
	command := exec.Command(os.Args[0], "-test.run=^TestFrontierMillionRestartHelper$", "-test.v")
	command.Env = append(os.Environ(), "DELVER_MILLION_HELPER_PATH="+params.Path, "DELVER_MILLION_HELPER_N="+strconv.Itoa(n), "DELVER_MILLION_HELPER_TERMINAL="+strconv.Itoa(terminal))
	output, e := command.CombinedOutput()
	require.NoError(t, e, "%s", output)
	t.Logf("restart helper: %s", output)
	before = time.Now()
	s, err = OpenStore(params)
	require.NoError(t, err)
	emit("unclean_process_recovery", map[string]interface{}{"reopen_seconds": time.Since(before).Seconds()})
	counts, err = s.Counts()
	require.NoError(t, err)
	require.Equal(t, int64(n+100), counts["admitted"])
	require.Equal(t, int64(64), counts["retry_pending"])
	for i := 0; i < 100; i++ {
		require.Equal(t, "pending", millionCandidate(t, s, fmt.Sprintf("http://crash.scale.invalid/%d", i)).State)
	}
	// Cleanup must leave retries and every other active URL intact.
	_, err = s.Cleanup(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	for uri := range retryOriginal {
		require.Equal(t, "retry_wait", millionCandidate(t, s, uri).State)
	}
	require.NoError(t, s.DrainRetries(ctx, 128))
	for uri, old := range retryOriginal {
		c := millionCandidate(t, s, uri)
		require.Equal(t, 1, c.Request.Attempt)
		require.NotEqual(t, old.Request.RequestID, c.Request.RequestID)
		require.NotEqual(t, old.Request.FrontierGeneration, c.Request.FrontierGeneration)
		c, err = s.Reserve(ctx, c)
		require.NoError(t, err)
		require.NoError(t, s.Published(c))
		require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: c.Request, Success: true, HTTPCode: 200}))
	}
	terminal += 64
	cleanupStart := time.Now()
	batches := 0
	for {
		cleaned, e := s.Cleanup(ctx, time.Now().Add(24*time.Hour))
		require.NoError(t, e)
		batches++
		if batches%50 == 0 {
			emit("expiring_terminal_history", map[string]interface{}{"cleanup_batches": batches})
		}
		if cleaned == 0 {
			break
		}
		require.Less(t, batches, n/100+100)
	}
	counts, err = s.Counts()
	require.NoError(t, err)
	require.Equal(t, int64(0), counts["retry_pending"])
	require.Equal(t, int64(n+100-terminal), counts["pending_and_active"])
	require.Equal(t, n+100-terminal, millionKeys(t, s, "u/"))
	require.Zero(t, millionKeys(t, s, "e/"))
	after, _, err := s.Pending(ctx, cursor, 128)
	require.NoError(t, err)
	require.Len(t, after, 128)
	for _, c := range after {
		require.Greater(t, c.Sequence, cursorSequence)
	}
	probe("after_terminal_expiration")
	emit("cleanup_complete", map[string]interface{}{"terminal_urls": terminal, "cleanup_seconds": time.Since(cleanupStart).Seconds(), "cleanup_batches": batches, "pending": counts["pending_and_active"]})
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: millionURI(0)}))
	counts, err = s.Counts()
	require.NoError(t, err)
	require.Equal(t, int64(n+101), counts["admitted"])
	// Verify explicit permanent retention against the same large database.
	require.NoError(t, s.Close())
	s = nil
	params.Retention.CompletedFor = 0
	params.Retention.FailedFor = 0
	params.Retention.DedupFor = 0
	s, err = OpenStore(params)
	require.NoError(t, err)
	permanent := "http://permanent.scale.invalid/"
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: permanent}))
	c := millionCandidate(t, s, permanent)
	c, err = s.Reserve(ctx, c)
	require.NoError(t, err)
	require.NoError(t, s.Published(c))
	require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: c.Request, Success: true, HTTPCode: 200}))
	_, err = s.Cleanup(ctx, time.Now().Add(365*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "completed", millionCandidate(t, s, permanent).State)
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: permanent}))
	counts, err = s.Counts()
	require.NoError(t, err)
	require.Equal(t, int64(n+102), counts["admitted"])
	emit("complete", map[string]interface{}{"complete": true, "terminal_urls": terminal + 1, "peak_heap_bytes": peakHeap, "peak_rss_bytes": peakRSS, "final_counts": counts})
}
func millionCandidateRead(tx *badger.Txn, uri string) Candidate {
	var c Candidate
	_ = read(tx, "u/"+uri, &c)
	return c
}

func TestFrontierMillionRestartHelper(t *testing.T) {
	path := os.Getenv("DELVER_MILLION_HELPER_PATH")
	if path == "" {
		t.Skip("scale test subprocess helper")
	}
	n, err := strconv.Atoi(os.Getenv("DELVER_MILLION_HELPER_N"))
	require.NoError(t, err)
	terminal, err := strconv.Atoi(os.Getenv("DELVER_MILLION_HELPER_TERMINAL"))
	require.NoError(t, err)
	s, err := OpenStore(millionParams(path, n))
	require.NoError(t, err)
	counts, err := s.Counts()
	require.NoError(t, err)
	require.Equal(t, int64(n), counts["admitted"])
	require.Equal(t, int64(terminal), counts["terminal_outcomes"])
	require.Equal(t, int64(64), counts["retry_pending"])
	for i := 0; i < 100; i++ {
		require.NoError(t, s.Admit(context.Background(), message.FetcherRequest{URI: fmt.Sprintf("http://crash.scale.invalid/%d", i)}))
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	raw, _ := json.Marshal(map[string]interface{}{"helper_admitted": 100, "heap_bytes": mem.HeapAlloc, "rss_bytes": residentBytes(), "unclean_exit": true})
	fmt.Println(string(raw))
	os.Exit(0)
}
