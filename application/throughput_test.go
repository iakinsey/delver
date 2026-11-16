package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/worker"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkspaceDurableRetryCompletesOnceWithoutInlineMultiplication(t *testing.T) {
	var hits int32
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "throughput-test-agent", r.UserAgent())
		if atomic.AddInt32(&hits, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html>recovered</html>")
	}))
	defer site.Close()
	workspace, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	require.NoError(t, err)
	defer workspace.Close()
	d := DefaultCrawlDefinition()
	d.Name = "retry integration"
	d.Seeds = []string{site.URL}
	d.Termination.WhenDrained = true
	no := false
	d.Network.RespectRobots = &no
	d.Network.UserAgent = "throughput-test-agent"
	d.RequestDelay = "0s"
	d.Fetch.SampleInterval = flow.Duration(10 * time.Millisecond)
	record := createWorkspaceCrawl(t, workspace, d)
	startWorkspaceCrawl(t, workspace, record)
	final := waitWorkspace(t, workspace, record["id"].(string), func(v map[string]interface{}) bool { return latestRun(v)["state"] == "completed" })
	require.Equal(t, int32(2), atomic.LoadInt32(&hits))
	require.Equal(t, "stopped", runtimeState(final))
}
func TestExpertFetchPatchPreservesOpaqueNumbersAndUpdatesRetryPolicy(t *testing.T) {
	RegisterWorker("fixture_opaque", func(json.RawMessage, map[string]interface{}) (worker.Worker, error) { return nil, nil })
	defer RegisterWorker("fixture_opaque", nil)
	for _, mode := range []string{"fixed", "adaptive"} {
		t.Run(mode, func(t *testing.T) {
			d := DefaultCrawlDefinition()
			d.Seeds = []string{"http://example.test/"}
			app, err := CompileCrawl(t.TempDir(), "test", d)
			require.NoError(t, err)
			app.Config.Fetch = nil
			for i, resource := range app.Resources {
				var params map[string]interface{}
				require.NoError(t, json.Unmarshal(resource.Parameters, &params))
				if resource.Type == "frontier_store" {
					delete(params, "retry")
				}
				if resource.Type == "crawl_queue" {
					delete(params, "accept_frontier_generations")
				}
				app.Resources[i].Parameters, _ = json.Marshal(params)
			}
			app.Workers[len(app.Workers)-1].Parameters = json.RawMessage(`{"opaque":9007199254740993123}`)
			// Opaque extension belongs to a custom worker which is intentionally accepted by Decode.
			app.Workers[len(app.Workers)-1].Type = "fixture_opaque"
			raw, err := json.Marshal(app)
			require.NoError(t, err)
			p := flow.Default()
			p.Mode = mode
			p.Concurrency = 8
			p.Maximum = 16
			p.FailureThreshold = 7
			request, _ := json.Marshal(map[string]interface{}{"configuration_json": string(raw), "fetch": p})
			patched, err := patchFetch(request)
			require.NoError(t, err)
			text := patched.(map[string]interface{})["configuration_json"].(string)
			require.Contains(t, text, "9007199254740993123")
			updated, err := Decode(strings.NewReader(text))
			require.NoError(t, err)
			require.Equal(t, p, *updated.Config.Fetch)
			for _, worker := range updated.Workers {
				if worker.Type == "http_fetcher" {
					require.Equal(t, p.PoolSize(), worker.Count)
				}
			}
			for _, resource := range updated.Resources {
				if resource.Type == "crawl_queue" {
					var params map[string]interface{}
					require.NoError(t, json.Unmarshal(resource.Parameters, &params))
					require.Equal(t, true, params["accept_frontier_generations"])
				}
				if resource.Type == "frontier_store" {
					var params frontier.StoreParams
					require.NoError(t, json.Unmarshal(resource.Parameters, &params))
					require.Equal(t, 7, params.Retry.FailureThreshold)
				}
			}
		})
	}
}

func TestDurableRetryRoutingAndQueueValidation(t *testing.T) {
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://example.test/"}
	app, err := CompileCrawl(t.TempDir(), "test", d)
	require.NoError(t, err)
	require.True(t, hasDurableRetries(app, "requests"))
	require.False(t, hasDurableRetries(app, "unrelated"))
	for i, resource := range app.Resources {
		if resource.Type == "crawl_queue" {
			var params map[string]interface{}
			require.NoError(t, json.Unmarshal(resource.Parameters, &params))
			params["accept_frontier_generations"] = false
			app.Resources[i].Parameters, _ = json.Marshal(params)
		}
	}
	err = Validate(app)
	require.Error(t, err)
	require.Contains(t, err.Error(), "accept_frontier_generations")
}

func TestDiskPressureChecksFutureOutputParents(t *testing.T) {
	free, err := diskFreeMB(filepath.Join(t.TempDir(), "future", "nested", "results.jsonl"))
	require.NoError(t, err)
	require.Greater(t, free, float64(0))
}

func TestCompiledCleanupUsesConfiguredRetentionCadence(t *testing.T) {
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://cadence.invalid/"}
	d.Retention.CleanupInterval = frontier.Lifetime(50 * time.Millisecond)
	app, err := CompileCrawl(t.TempDir(), "cadence", d)
	require.NoError(t, err)
	for _, w := range app.Workers {
		if w.Type == "frontier_cleanup" {
			require.Equal(t, 50*time.Millisecond, w.Interval)
			return
		}
	}
	t.Fatal("missing cleanup worker")
}

func TestConfiguredFastRetentionActuallyExpiresCrawlHistory(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html>retention cadence</html>")
	}))
	defer site.Close()
	workspace, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	require.NoError(t, err)
	defer workspace.Close()
	d := DefaultCrawlDefinition()
	d.Name = "fast cleanup"
	d.Seeds = []string{site.URL}
	d.RequestDelay = "0s"
	d.Revisit = "10s"
	no := false
	d.Network.RespectRobots = &no
	d.Retention = frontier.Retention{CompletedFor: frontier.Lifetime(100 * time.Millisecond), FailedFor: frontier.Lifetime(100 * time.Millisecond), DedupFor: frontier.Lifetime(300 * time.Millisecond), CleanupInterval: frontier.Lifetime(50 * time.Millisecond), CleanupBatchSize: 128}
	record := createWorkspaceCrawl(t, workspace, d)
	startWorkspaceCrawl(t, workspace, record)
	waitWorkspace(t, workspace, record["id"].(string), func(v map[string]interface{}) bool {
		outer, _ := v["runtime"].(map[string]interface{})
		frame, _ := outer["runtime"].(map[string]interface{})
		front, _ := frame["frontier"].(map[string]interface{})
		cleaned, _ := front["cleaned"].(float64)
		return cleaned >= 2
	})
}

func TestPressureSamplingDoesNotReadMutableHTTPClient(t *testing.T) {
	root := t.TempDir()
	p := &preparedApplication{app: config.Application{Config: config.LoadConfig()}, resources: map[string]interface{}{}}
	p.app.Config.API.OperationsPath = filepath.Join(root, "state.json")
	p.pressurePaths = append(pipelinePaths(p.app), root)
	var writers sync.WaitGroup
	writers.Add(1)
	go func() {
		defer writers.Done()
		for i := 0; i < 100000; i++ {
			p.app.Config.HTTPClient.ConnectTimeout = time.Duration(i)
		}
	}()
	for i := 0; i < 100; i++ {
		require.True(t, p.pressure().DiskKnown)
	}
	writers.Wait()
}

func TestStoppedRunSummaryIncludesFinishingAttempts(t *testing.T) {
	entered := make(chan struct{}, 1)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-r.Context().Done() }))
	defer site.Close()
	workspace, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	require.NoError(t, err)
	defer workspace.Close()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{site.URL}
	no := false
	d.Network.RespectRobots = &no
	d.Network.RequestTimeout = "30s"
	d.Fetch.Concurrency = 1
	record := createWorkspaceCrawl(t, workspace, d)
	id := record["id"].(string)
	startWorkspaceCrawl(t, workspace, record)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("fetch never entered")
	}
	workspaceCall(t, workspace, "command", map[string]string{"crawl_id": id, "action": "stop", "id": "stop-summary"})
	final := waitWorkspace(t, workspace, id, func(v map[string]interface{}) bool { return runtimeState(v) == "stopped" })
	run := latestRun(final)
	raw, err := os.ReadFile(filepath.Join(workspace.runRoot(id, run["id"].(string)), "state.json.fetch.json"))
	require.NoError(t, err)
	var saved struct {
		Totals flow.Totals `json:"totals"`
	}
	require.NoError(t, json.Unmarshal(raw, &saved))
	require.Equal(t, int64(1), saved.Totals.Attempts)
	summaryBytes, err := json.Marshal(run["summary"])
	require.NoError(t, err)
	var summary RuntimeSnapshot
	require.NoError(t, json.Unmarshal(summaryBytes, &summary))
	require.Equal(t, saved.Totals, summary.Fetch.Totals, "stopped summary lost finishing attempts")
	for name, activity := range summary.Workers {
		require.Zero(t, activity.Active, name)
	}
	require.Zero(t, summary.Fetch.Active)
	require.NoError(t, workspace.Close())
	reopened, err := NewWorkspace(context.Background(), Options{DataDirectory: workspace.root})
	require.NoError(t, err)
	defer reopened.Close()
	persisted := workspaceCall(t, reopened, "detail", map[string]string{"crawl_id": id}).(map[string]interface{})
	persistedBytes, err := json.Marshal(latestRun(persisted)["summary"])
	require.NoError(t, err)
	var restored RuntimeSnapshot
	require.NoError(t, json.Unmarshal(persistedBytes, &restored))
	require.Equal(t, saved.Totals, restored.Fetch.Totals)
	for name, activity := range restored.Workers {
		require.Zero(t, activity.Active, name)
	}
}
