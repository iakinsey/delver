package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/report"
	"github.com/iakinsey/delver/trap"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceRestoresParkedDiscoveriesFromCompletedRun(t *testing.T) {
	ctx := context.Background()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<h1>Harmless fixture</h1>")
	}))
	defer proxy.Close()
	workspace, e := NewWorkspace(ctx, Options{DataDirectory: t.TempDir()})
	require.NoError(t, e)
	defer workspace.Close()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://blocked.test/page"}
	d.Traps.MinimumSamples = 2
	d.Proxy = ProxySettings{Mode: "http", URL: proxy.URL}
	no := false
	d.Network.RespectRobots = &no
	d.RequestDelay = "0s"
	d.Termination.WhenDrained = true
	record := createWorkspaceCrawl(t, workspace, d)
	id := record["id"].(string)
	runID := "restored-run"
	root := workspace.runRoot(id, runID)
	app, e := CompileCrawl(root, runID, d)
	require.NoError(t, e)
	var params frontier.StoreParams
	for _, resource := range app.Resources {
		if resource.Type == "frontier_store" {
			require.NoError(t, json.Unmarshal(resource.Parameters, &params))
		}
	}
	require.Equal(t, filepath.Join(root, "frontier"), params.Path)
	store, e := frontier.OpenStore(params)
	require.NoError(t, e)
	require.NoError(t, store.StageDiscovery(ctx, "before-trap", []message.FetcherRequest{{URI: d.Seeds[0]}}))
	sites := []string{"blocked.test", "peer-a.test", "peer-b.test"}
	excluded := false
	for page := 0; page < 12 && !excluded; page++ {
		for member, site := range sites {
			var body strings.Builder
			body.WriteString("<html><body><main><article><h1>Fixture</h1><p>Harmless material</p><nav>")
			links := []string{}
			for n := 0; n < 12; n++ {
				link := fmt.Sprintf("http://%s/page/%d/%d", sites[(member+n+1)%3], page, n)
				links = append(links, link)
				fmt.Fprintf(&body, `<a href="%s">Link</a>`, link)
			}
			body.WriteString("</nav></article></main></body></html>")
			observation, e := trap.Observe(ctx, strings.NewReader(body.String()), fmt.Sprintf("http://%s/page/%d", site, page), links)
			require.NoError(t, e)
			blocked, e := store.ObserveTrap(ctx, observation)
			require.NoError(t, e)
			if site == sites[0] && blocked {
				excluded = true
			}
		}
	}
	require.True(t, excluded, "fixture never excluded the target")
	_, e = store.DrainDiscovery(ctx, 128)
	require.NoError(t, e)
	counts, e := store.Counts()
	require.NoError(t, e)
	require.EqualValues(t, 1, counts["trap_parked"])
	require.Zero(t, counts["deferred_discoveries"])
	require.NoError(t, store.Close())
	workspace.mu.Lock()
	workspace.records[id].Runs = append(workspace.records[id].Runs, CrawlRun{ID: runID, Definition: d, State: "completed", Desired: "stopped"})
	require.NoError(t, workspace.saveLocked())
	workspace.mu.Unlock()
	graph, e := report.Open(app.Config.API.OperationsPath+".report.sqlite", false)
	require.NoError(t, e)
	require.NoError(t, graph.Discover(ctx, []report.URL{
		{URI: "https://root.test/"},
		{URI: d.Seeds[0], Origin: "https://root.test/"},
	}))
	require.NoError(t, graph.Close())
	request := map[string]interface{}{"crawl_id": id, "run_id": runID, "host": "root.test", "hide_traps": true}
	filtered := workspaceCall(t, workspace, "host_graph", request).(map[string]interface{})
	require.EqualValues(t, 1, filtered["hidden_hosts"])
	require.Len(t, filtered["nodes"], 1)
	workspaceCall(t, workspace, "trap_restore", map[string]interface{}{"crawl_id": id, "site": sites[0]})
	restored := workspaceCall(t, workspace, "host_graph", request).(map[string]interface{})
	require.Zero(t, restored["hidden_hosts"])
	require.Len(t, restored["nodes"], 2)
	store, e = frontier.OpenStore(params)
	require.NoError(t, e)
	counts, e = store.Counts()
	require.NoError(t, e)
	require.EqualValues(t, 1, counts["deferred_discoveries"])
	require.Zero(t, counts["trap_parked"])
	require.NoError(t, store.Close())
	startWorkspaceCrawl(t, workspace, record)
	detail := waitWorkspace(t, workspace, id, func(v map[string]interface{}) bool { return latestRun(v)["state"] == "completed" })
	require.Len(t, detail["runs"], 1)
	require.Equal(t, runID, latestRun(detail)["id"])
	summary := latestRun(detail)["summary"].(map[string]interface{})
	totals := summary["fetch"].(map[string]interface{})["totals"].(map[string]interface{})
	require.Equal(t, "1", fmt.Sprint(totals["successes"]))
}
