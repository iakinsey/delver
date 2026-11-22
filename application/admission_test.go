package application

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
)

func TestAdmissionSaturationTraversal(t *testing.T) {
	for _, strategy := range []string{"breadth_first", "depth_first", "host_diverse", "adaptive"} {
		for _, mode := range []string{"fixed", "adaptive"} {
			t.Run(strategy+"/"+mode, func(t *testing.T) {
				const pages = 48
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					page, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/page/"))
					if err != nil || page < 0 || page >= pages {
						http.Error(w, "unexpected fixture request", 400)
						return
					}
					w.Header().Set("Content-Type", "text/html")
					fmt.Fprintf(w, "<title>page %d</title>", page)
					for _, next := range []int{page + 1, page + 7} {
						if next < pages {
							fmt.Fprintf(w, `<a href="http://host-%d.admission-%d.invalid/page/%d">next</a>`, next%4, next%4, next)
						}
					}
				}))
				defer proxy.Close()
				d := DefaultCrawlDefinition()
				d.Name, d.Scope, d.Strategy = "admission regression", "open_web", strategy
				d.Seeds = []string{"http://host-0.admission-0.invalid/page/0"}
				d.Proxy = ProxySettings{Mode: "http", URL: proxy.URL}
				no, zero := false, 0
				d.Network.RespectRobots, d.Network.MaxRetries = &no, &zero
				d.RequestDelay, d.MaxDepth, d.MaxHops = "0s", 64, 64
				d.Termination.WhenDrained = true
				d.Fetch.Mode, d.Fetch.Concurrency, d.Fetch.Maximum = mode, 8, 16
				d.Fetch.SampleInterval = flow.Duration(25 * time.Millisecond)
				d.Fetch.AdjustmentInterval = flow.Duration(100 * time.Millisecond)
				d.Fetch.DownstreamHigh, d.Fetch.DownstreamLow = 4, 2
				app, err := CompileCrawl(t.TempDir(), "admission-regression", d)
				require.NoError(t, err)
				for i := range app.Resources {
					if app.Resources[i].Type == "frontier_store" {
						var p frontier.StoreParams
						require.NoError(t, json.Unmarshal(app.Resources[i].Parameters, &p))
						p.MaxPending, p.MaxPendingPerOrigin = 8, 2
						app.Resources[i].Parameters, err = json.Marshal(p)
						require.NoError(t, err)
					}
				}
				s, err := NewSupervisor(context.Background(), app)
				require.NoError(t, err)
				defer s.Close()
				require.NoError(t, s.Execute(context.Background(), "start", "", ""))
				deadline := time.Now().Add(15 * time.Second)
				lastAttempt, lastProgress := int64(0), time.Now()
				complete := false
				for time.Now().Before(deadline) {
					frame := s.control.snapshot()
					if frame.Fetch == nil {
						time.Sleep(10 * time.Millisecond)
						continue
					}
					require.Empty(t, frame.Health)
					require.LessOrEqual(t, frame.Frontier["pending_and_active"], int64(8))
					if frame.Fetch.Totals.Attempts != lastAttempt {
						lastAttempt, lastProgress = frame.Fetch.Totals.Attempts, time.Now()
					}
					if lastAttempt > 0 && frame.Fetch.Reason == "capacity available" && frame.Frontier["pending_and_active"] > 0 {
						require.Less(t, time.Since(lastProgress), 2*time.Second, "runnable fixture stalled after gates cleared")
					}
					if frame.Frontier["terminal_outcomes"] == pages && frame.Frontier["deferred_discoveries"] == 0 && frame.Frontier["pending_and_active"] == 0 {
						require.EqualValues(t, pages, frame.Fetch.Totals.Successes)
						complete = true
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !complete {
					f := s.control.snapshot()
					t.Logf("final frontier=%v queues=%v fetch=%+v", f.Frontier, f.Queues, f.Fetch.Totals)
				}
				require.True(t, complete, "saturated traversal did not finish")
				require.NoError(t, s.Close())
			})
		}
	}
}

func TestStartupRecoversConfirmedQueueIdentity(t *testing.T) {
	var hits int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<title>repaired</title>")
	}))
	defer proxy.Close()
	d := DefaultCrawlDefinition()
	d.Name = "identity recovery"
	d.Scope = "open_web"
	d.Seeds = []string{"http://recovery.invalid/page"}
	d.Proxy = ProxySettings{Mode: "http", URL: proxy.URL}
	no := false
	d.Network.RespectRobots = &no
	d.Termination.WhenDrained = true
	d.RequestDelay = "0s"
	app, err := CompileCrawl(t.TempDir(), "identity-recovery", d)
	require.NoError(t, err)
	var sp frontier.StoreParams
	var qp queue.CrawlQueueParams
	for i := range app.Resources {
		if app.Resources[i].Type == "frontier_store" {
			require.NoError(t, json.Unmarshal(app.Resources[i].Parameters, &sp))
			sp.MaxPending = 1
			app.Resources[i].Parameters, err = json.Marshal(sp)
			require.NoError(t, err)
		}
		if app.Resources[i].Type == "crawl_queue" {
			require.NoError(t, json.Unmarshal(app.Resources[i].Parameters, &qp))
		}
	}
	store, err := frontier.OpenStore(sp)
	require.NoError(t, err)
	require.NoError(t, store.Admit(context.Background(), message.FetcherRequest{URI: d.Seeds[0]}))
	rows, _, err := store.Pending(context.Background(), "", 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	candidate, err := store.Reserve(context.Background(), rows[0])
	require.NoError(t, err)
	q, err := queue.NewCrawlQueue(qp)
	require.NoError(t, err)
	wrong := candidate.Request
	wrong.RequestID = "old-generation-request"
	body, err := json.Marshal(wrong)
	require.NoError(t, err)
	require.NoError(t, q.Put(types.Message{ID: "old-generation-request", MessageType: types.FetcherRequestType, Message: body}, 0))
	require.NoError(t, store.Published(candidate))
	require.NoError(t, q.Start())
	select {
	case delivered := <-q.GetChannel():
		require.NoError(t, q.EndTransaction(delivered, true))
	case <-time.After(time.Second):
		t.Fatal("fixture delivery missing")
	}
	require.NoError(t, q.(interface{ Close() error }).Close())
	require.NoError(t, store.Close())
	s, err := NewSupervisor(context.Background(), app)
	require.NoError(t, err)
	defer func() { s.Close() }()
	require.NoError(t, s.Execute(context.Background(), "start", "", ""))
	require.Eventually(t, func() bool {
		frame := s.control.snapshot()
		return frame.Fetch != nil && frame.Fetch.Totals.Successes == 1 && frame.Frontier["pending_and_active"] == 0 && frame.Frontier["deferred_discoveries"] == 0
	}, 8*time.Second, 20*time.Millisecond, "startup did not recover the stranded generation")
	require.EqualValues(t, 1, atomic.LoadInt64(&hits))
	require.NoError(t, s.Close())
	s, err = NewSupervisor(context.Background(), app)
	require.NoError(t, err)
	require.NoError(t, s.Execute(context.Background(), "start", "", ""))
	time.Sleep(250 * time.Millisecond)
	require.EqualValues(t, 1, atomic.LoadInt64(&hits), "valid completed identity was replayed on the next startup")
	require.NoError(t, s.Close())
}
