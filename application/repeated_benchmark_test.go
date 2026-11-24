package application

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// This fixture intentionally uses only pre-7798c4e interfaces so the identical
// workload can be compiled into the original baseline and final test binaries.
func BenchmarkRepeatedFamilyCapacity(b *testing.B) {
	for _, kind := range []string{"unique", "repeated"} {
		b.Run(kind, func(b *testing.B) {
			const sites = 20
			const pages = 5
			bodies := map[string]string{}
			for host := 0; host < sites; host++ {
				for page := 0; page < pages; page++ {
					var body strings.Builder
					body.WriteString("<html><head><title>Fixture</title></head><body><main><article><h1>Directory</h1><section><p>")
					seed := fmt.Sprintf("%d-%d", host, page)
					if kind == "repeated" {
						seed = "shared"
					}
					for word := 0; word < 220; word++ {
						h := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", seed, word)))
						fmt.Fprintf(&body, "%x ", h[:5])
					}
					fmt.Fprintf(&body, "http://capacity-%02d.test/%d", host, page)
					body.WriteString("</p></section><ul>")
					for target := 0; target < sites; target++ {
						for next := 0; next < pages; next++ {
							fmt.Fprintf(&body, `<li><a href="http://capacity-%02d.test/%d">Resource</a></li>`, target, next)
						}
					}
					body.WriteString("</ul></article></main></body></html>")
					bodies[fmt.Sprintf("capacity-%02d.test/%d", host, page)] = body.String()
				}
			}
			var hits atomic.Int64
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, bodies[r.URL.Host+r.URL.Path])
			}))
			defer proxy.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				workspace, err := NewWorkspace(context.Background(), Options{DataDirectory: b.TempDir()})
				if err != nil {
					b.Fatal(err)
				}
				d := DefaultCrawlDefinition()
				d.Name = "capacity"
				d.Seeds = []string{"http://capacity-00.test/0"}
				d.Scope = "open_web"
				d.Traps.Enabled = false
				d.Proxy = ProxySettings{Mode: "http", URL: proxy.URL}
				no := false
				d.Network.RespectRobots = &no
				d.RequestDelay = "0s"
				d.Concurrency = 32
				d.Termination.WhenDrained = true
				if d.Fetch != nil {
					d.Fetch.Mode = "fixed"
					d.Fetch.Concurrency = 16
				}
				raw := mustBenchmarkJSON(map[string]interface{}{"definition": d})
				created, err := workspace.HandleCrawls(context.Background(), "create", raw)
				if err != nil {
					workspace.Close()
					b.Fatal(err)
				}
				record := created.(map[string]interface{})
				id := fmt.Sprint(record["id"])
				_, err = workspace.HandleCrawls(context.Background(), "start_run", mustBenchmarkJSON(map[string]interface{}{"crawl_id": id, "revision": 1}))
				if err != nil {
					workspace.Close()
					b.Fatal(err)
				}
				deadline := time.Now().Add(60 * time.Second)
				for {
					workspace.mu.Lock()
					r := workspace.records[id]
					state := ""
					if len(r.Runs) > 0 {
						state = r.Runs[len(r.Runs)-1].State
					}
					workspace.mu.Unlock()
					if state == "completed" {
						break
					}
					if state == "failed" || time.Now().After(deadline) {
						snapshot, _ := workspace.snapshot(id)
						raw, _ := json.Marshal(snapshot)
						b.Logf("incomplete snapshot: %s", raw)
						workspace.Close()
						b.Fatal("crawl did not complete", state)
					}
					time.Sleep(5 * time.Millisecond)
				}
				if err = workspace.Close(); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(hits.Load())/b.Elapsed().Seconds(), "pages/s")
		})
	}
}

func mustBenchmarkJSON(v interface{}) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}
