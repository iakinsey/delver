package application

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The same self-contained benchmark runs against the checkpoint and final code.
// Each response is unique HTML; all pipeline stages, durable queues, and JSONL
// output execute. No competing workload should run during comparisons.
func BenchmarkWorkspaceCapacity(b *testing.B)           { benchmarkWorkspaceCapacity(b, false) }
func BenchmarkWorkspaceCapacityWithReport(b *testing.B) { benchmarkWorkspaceCapacity(b, true) }
func benchmarkWorkspaceCapacity(b *testing.B, showReport bool) {
	const pages = 128
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(20 * time.Millisecond):
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "<title>Page %s</title><p>Unique content %s</p>", r.URL.Path, r.URL.Path)
	}))
	defer site.Close()
	var elapsed time.Duration
	var last *RuntimeSnapshot
	for i := 0; i < b.N; i++ {
		w, err := NewWorkspace(context.Background(), Options{DataDirectory: b.TempDir()})
		if err != nil {
			b.Fatal(err)
		}
		d := DefaultCrawlDefinition()
		d.Name = "capacity"
		d.Scope = "open_web"
		d.RequestDelay = "0s"
		d.Termination.WhenDrained = true
		d.Fetch.Mode = "fixed"
		d.Fetch.Concurrency = 32
		d.Fetch.Maximum = 32
		no := false
		d.Network.RespectRobots = &no
		d.Proxy = ProxySettings{Mode: "http", URL: site.URL}
		for j := 0; j < pages; j++ {
			d.Seeds = append(d.Seeds, fmt.Sprintf("http://benchmark-%d.invalid/page/%d", j, j))
		}
		raw, _ := json.Marshal(map[string]interface{}{"id": fmt.Sprint(i), "definition": d})
		value, err := w.HandleCrawls(context.Background(), "create", raw)
		if err != nil {
			b.Fatal(err)
		}
		encoded, _ := json.Marshal(value)
		var record struct {
			ID       string `json:"id"`
			Revision uint64 `json:"revision"`
		}
		if err = json.Unmarshal(encoded, &record); err != nil {
			b.Fatal(err)
		}
		raw, _ = json.Marshal(map[string]interface{}{"crawl_id": record.ID, "revision": record.Revision, "id": "start"})
		started := time.Now()
		_, err = w.HandleCrawls(context.Background(), "start_run", raw)
		if err != nil {
			b.Fatal(err)
		}
		complete := false
		for time.Since(started) < 30*time.Second {
			value, err = w.snapshot(record.ID)
			if err != nil {
				b.Fatal(err)
			}

			encoded, _ = json.Marshal(value)
			var state struct {
				Runtime struct {
					Runtime *RuntimeSnapshot `json:"runtime"`
				} `json:"runtime"`
				Runs []struct {
					State   string           `json:"state"`
					Summary *RuntimeSnapshot `json:"summary"`
				} `json:"runs"`
			}
			if err = json.Unmarshal(encoded, &state); err != nil {
				b.Fatal(err)
			}

			if showReport && len(state.Runs) > 0 && (state.Runs[0].State == "completed" || state.Runtime.Runtime != nil && state.Runtime.Runtime.Fetch != nil) {
				rawReport, _ := json.Marshal(map[string]interface{}{"crawl_id": record.ID})
				if _, err = w.HandleCrawls(context.Background(), "report", rawReport); err != nil {
					b.Fatal(err)
				}
				rawGraph, _ := json.Marshal(map[string]interface{}{"crawl_id": record.ID, "host": "benchmark-0.invalid", "depth": 2})
				if _, err = w.HandleCrawls(context.Background(), "host_graph", rawGraph); err != nil {
					b.Fatal(err)
				}
			}
			if len(state.Runs) == 1 && state.Runs[0].State == "completed" {
				last = state.Runs[0].Summary
				complete = true
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		elapsed += time.Since(started)
		if err = w.Close(); err != nil {
			b.Fatal(err)
		}
		if !complete || last == nil || last.Fetch == nil {
			b.Fatal("pipeline did not complete")
		}
		if last.Fetch.Totals.Successes != pages {
			b.Fatalf("successes %d != %d", last.Fetch.Totals.Successes, pages)
		}
		if last.Frontier["unique_html"] != pages {
			b.Fatalf("unique HTML: %v", last.Frontier)
		}
	}
	b.ReportMetric(float64(pages*b.N)/elapsed.Seconds(), "useful_pages/s")
	b.ReportMetric(float64(pages*b.N)/elapsed.Seconds(), "unique_HTML/s")
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	b.ReportMetric(float64(memory.HeapAlloc)/(1<<20), "heap_MiB")
	b.ReportMetric(float64(runtime.NumGoroutine()), "goroutines")
	if data, err := os.ReadFile("/proc/self/status"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "VmRSS:") {
				fields := strings.Fields(line)
				value, _ := strconv.ParseFloat(fields[1], 64)
				b.ReportMetric(value/1024, "RSS_MiB")
			}
		}
	}
	b.ReportMetric(last.Fetch.Latencies["attempt"].P50, "p50_ms")
	b.ReportMetric(last.Fetch.Latencies["attempt"].P95, "p95_ms")
	b.ReportMetric(last.Fetch.Latencies["frontier_storage"].P95, "storage_p95_ms")
	raw, _ := json.Marshal(map[string]interface{}{"configuration": map[string]interface{}{"mode": "fixed", "slots": 32, "pages": pages, "response_delay": "20ms", "proxy": "controlled-loopback", "completion": "drained"}, "final_metrics": last})
	b.Log(string(raw))
}
