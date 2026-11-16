package util

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/iakinsey/delver/flow"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkFetchCapacity isolates bounded network admission against identical,
// deterministic mixed-latency destinations. It does not predict Tor capacity.
func BenchmarkFetchCapacity(b *testing.B) {
	for _, slots := range []int{4, 16, 32, 64} {
		b.Run(fmt.Sprint(slots), func(b *testing.B) {
			var active, peak int64
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/offline" {
					<-r.Context().Done()
					return
				}
				delay := 20 * time.Millisecond
				if r.URL.Path == "/slow" {
					delay = 60 * time.Millisecond
				}
				select {
				case <-r.Context().Done():
					return
				case <-time.After(delay):
				}
				if r.URL.Path == "/slow" {
					w.WriteHeader(503)
				} else {
					w.Header().Set("Content-Type", "text/html")
					fmt.Fprint(w, "<html>ok</html>")
				}
			}))
			defer proxy.Close()
			p := flow.Default()
			p.Concurrency = slots
			p.Maximum = slots
			control, err := flow.New(flow.WithBroker(context.Background(), flow.NewBroker(flow.ExecutionLimits{Concurrency: slots, HostConcurrency: slots})), b.Name(), p, nil)
			if err != nil {
				b.Fatal(err)
			}
			defer control.Close()
			c := phaseClient()
			c.HTTPProxyUrl = proxy.URL
			c.Flow = control
			c.RequestDelay = 0
			c.Timeout = 250 * time.Millisecond
			client := NewHTTPClientWithConfig(c)
			b.ResetTimer()
			start := time.Now()
			for run := 0; run < b.N; run++ {
				var wg sync.WaitGroup
				for i := 0; i < 256; i++ {
					wg.Add(1)
					go func(i int) {
						defer wg.Done()
						release, e := control.Acquire(context.Background())
						if e != nil {
							b.Error(e)
							return
						}
						defer release()
						admitted := atomic.AddInt64(&active, 1)
						defer atomic.AddInt64(&active, -1)
						for {
							old := atomic.LoadInt64(&peak)
							if admitted <= old || atomic.CompareAndSwapInt64(&peak, old, admitted) {
								break
							}
						}

						path := "/ok"
						if i%4 == 0 {
							path = "/slow"
						}
						if i%8 == 0 {
							path = "/offline"
						}
						started := time.Now()
						resp, e := client.Perform(fmt.Sprintf("http://host-%d.invalid%s", i, path))
						if e != nil {
							if path == "/offline" && errors.Is(e, context.DeadlineExceeded) {
								control.Record(0, false, false, "timeout", time.Since(started), 0, 0)
								return
							}
							b.Error(e)
							return
						}

						n, e := io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
						if e != nil {
							b.Error(e)
						}
						control.Record(resp.StatusCode, resp.StatusCode == 200, resp.StatusCode == 200, "", time.Since(started), n, n)
					}(i)
				}
				wg.Wait()
			}
			elapsed := time.Since(start).Seconds()
			snapshot := control.Snapshot()
			b.ReportMetric(float64(192*b.N)/elapsed, "useful_pages/s")
			b.ReportMetric(float64(256*b.N)/elapsed, "attempts/s")
			b.ReportMetric(float64(atomic.LoadInt64(&peak)), "peak_requests")
			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)
			b.ReportMetric(float64(mem.HeapAlloc)/(1<<20), "heap_MiB")
			b.ReportMetric(snapshot.Latencies["attempt"].P95, "p95_ms")
			b.ReportMetric(snapshot.Latencies["attempt"].P50, "p50_ms")
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
			raw, _ := json.Marshal(map[string]interface{}{"slots": slots, "metrics": snapshot})
			b.Log(string(raw))

			if peak > int64(slots) {
				b.Fatal("capacity exceeded")
			}
		})
	}
}
