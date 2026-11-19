package application

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iakinsey/delver/flow"
	"github.com/stretchr/testify/require"
)

const discoveryTestAgent = "https://en.wikipedia.org/wiki/Hatsune_Miku"

type discoveryMeasurement struct {
	ElapsedMS, FetchIdleMS              float64
	AnalyzedPeak                        int64
	StorageOperations, Skips, Successes int64
}

// This workload exercises real workers, durable queues and the downstream gate.
// It deliberately emits link-heavy pages; excluded destinations must never be fetched.
func measureDiscoveryPressure(tb testing.TB, blocked, allowed int) discoveryMeasurement {
	tb.Helper()
	const roots = 16
	var badAgent, excludedHits atomic.Int64
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.UserAgent() != discoveryTestAgent {
			badAgent.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, ".pdf") {
			excludedHits.Add(1)
		}
		w.Header().Set("Content-Type", "text/html")
		if strings.HasPrefix(r.Host, "root-") {
			fmt.Fprintf(w, "<title>%s</title>", r.URL.Path)
			for i := 0; i < blocked; i++ {
				fmt.Fprintf(w, `<a href="http://excluded.invalid%s/%d.pdf">file</a>`, r.URL.Path, i)
			}
			for i := 0; i < allowed; i++ {
				fmt.Fprintf(w, `<a href="http://allowed-%d.invalid%s/%d.html">page</a>`, i, r.URL.Path, i)
			}
		} else {
			fmt.Fprintf(w, "<title>Unique %s</title><p>Content %s</p>", r.URL.Path, r.URL.Path)
		}
	}))
	defer site.Close()
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: tb.TempDir()})
	require.NoError(tb, err)
	defer func() { require.NoError(tb, w.Close()) }()
	d := DefaultCrawlDefinition()
	d.Name, d.Scope, d.RequestDelay = "Discovery pressure", "open_web", "0s"
	d.Network.RespectRobots = boolPointer(false)
	d.Network.UserAgent = discoveryTestAgent
	d.Proxy = ProxySettings{Mode: "http", URL: site.URL}
	d.ExcludedExtensions = "pdf"
	d.Fetch.Mode, d.Fetch.Concurrency, d.Fetch.Maximum = "fixed", 16, 16
	d.Fetch.DownstreamHigh, d.Fetch.DownstreamLow = 8, 4
	d.Fetch.SampleInterval = flow.Duration(20 * time.Millisecond)
	d.Termination.WhenDrained = true
	for i := 0; i < roots; i++ {
		d.Seeds = append(d.Seeds, fmt.Sprintf("http://root-%d.invalid/root/%d", i, i))
	}
	call := func(action string, payload interface{}) interface{} {
		raw, e := json.Marshal(payload)
		require.NoError(tb, e)
		value, e := w.HandleCrawls(context.Background(), action, raw)
		require.NoError(tb, e)
		return value
	}
	value := call("create", map[string]interface{}{"definition": d})
	raw, err := json.Marshal(value)
	require.NoError(tb, err)
	var record struct {
		ID       string `json:"id"`
		Revision uint64 `json:"revision"`
	}
	require.NoError(tb, json.Unmarshal(raw, &record))
	started, previous := time.Now(), time.Now()
	call("start_run", map[string]interface{}{"crawl_id": record.ID, "revision": record.Revision})
	var result discoveryMeasurement
	for time.Since(started) < 90*time.Second {
		value, e := w.snapshot(record.ID)
		require.NoError(tb, e)
		raw, e := json.Marshal(value)
		require.NoError(tb, e)
		var state struct {
			Runtime struct {
				Runtime *RuntimeSnapshot `json:"runtime"`
			} `json:"runtime"`
			Runs []CrawlRun `json:"runs"`
		}
		require.NoError(tb, json.Unmarshal(raw, &state))
		now := time.Now()
		if frame := state.Runtime.Runtime; frame != nil && frame.Fetch != nil {
			require.Empty(tb, frame.Health)
			if frame.Queues["analyzed"] > result.AnalyzedPeak {
				result.AnalyzedPeak = frame.Queues["analyzed"]
			}
			if frame.Fetch.Reason == "downstream_high" && frame.Fetch.NetworkActive == 0 {
				result.FetchIdleMS += float64(now.Sub(previous).Microseconds()) / 1000
			}
		}
		previous = now
		if len(state.Runs) > 0 && state.Runs[0].State == "completed" {
			final := state.Runs[0].Summary
			require.NotNil(tb, final)
			require.NotNil(tb, final.Fetch)
			result.ElapsedMS = float64(time.Since(started).Microseconds()) / 1000
			result.StorageOperations = final.Fetch.Counters["storage_operations"]
			result.Skips, result.Successes = final.Frontier["policy_skips"], final.Fetch.Totals.Successes
			require.GreaterOrEqual(tb, result.Skips, int64(roots*blocked))
			require.EqualValues(tb, roots*(allowed+1), result.Successes)
			require.Zero(tb, final.Frontier["pending_and_active"])
			require.Zero(tb, final.Frontier["deferred_discoveries"])
			require.Zero(tb, badAgent.Load())
			require.Zero(tb, excludedHits.Load())
			return result
		}
		time.Sleep(20 * time.Millisecond)
	}
	tb.Fatal("discovery workload failed to drain")
	return result
}

func TestExtensionDiscoveryPressureDrains(t *testing.T) {
	for _, mode := range []struct {
		name             string
		blocked, allowed int
	}{{"excluded-heavy", 4096, 1}, {"mixed", 32, 32}, {"allowed", 0, 32}} {
		t.Run(mode.name, func(t *testing.T) {
			v := measureDiscoveryPressure(t, mode.blocked, mode.allowed)
			require.EqualValues(t, 16*mode.blocked, v.Skips)
			t.Logf("measurement: %+v", v)
		})
	}
}

func BenchmarkWorkspaceDiscoveryPressure(b *testing.B) {
	for _, mode := range []struct {
		name             string
		blocked, allowed int
	}{{"excluded-heavy", 4096, 1}, {"mixed", 32, 32}, {"allowed", 0, 32}} {
		b.Run(mode.name, func(b *testing.B) {
			var total discoveryMeasurement
			for i := 0; i < b.N; i++ {
				v := measureDiscoveryPressure(b, mode.blocked, mode.allowed)
				total.ElapsedMS += v.ElapsedMS
				total.FetchIdleMS += v.FetchIdleMS
				total.Skips += v.Skips
				total.StorageOperations += v.StorageOperations
				total.Successes += v.Successes
				if v.AnalyzedPeak > total.AnalyzedPeak {
					total.AnalyzedPeak = v.AnalyzedPeak
				}
			}
			b.ReportMetric(total.ElapsedMS/float64(b.N), "pipeline_ms/run")
			b.ReportMetric(total.FetchIdleMS/float64(b.N), "fetch_idle_ms/run")
			b.ReportMetric(float64(total.StorageOperations)/float64(b.N), "storage_ops/run")
			b.ReportMetric(float64(total.AnalyzedPeak), "analyzed_peak")
			b.ReportMetric(float64(total.Skips)/float64(b.N), "policy_skips/run")
			b.ReportMetric(float64(total.Successes)*1000/total.ElapsedMS, "successes/s")
		})
	}
}
