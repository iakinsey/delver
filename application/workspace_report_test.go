package application

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceReportRecordsHTTPReceiptsAndHostEdges(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			fmt.Fprint(w, "User-agent: *\nAllow: /\n")
		case "/root":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<title>Root</title><a href="http://b.report.test/page">B</a><a href="http://c.report.test/ref">C</a><a href="/redirect">Redirect</a><a href="/badbody">Body</a><a href="/timeout">Timeout</a><a href="/deadredirect">Dead redirect</a>`)
		case "/ref":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<title>Reference</title><a href="http://b.report.test/page">B again</a>`)
		case "/redirect":
			http.Redirect(w, r, "http://c.report.test/missing", 302)
		case "/deadredirect":
			http.Redirect(w, r, "http://d.report.test/timeout", 302)
		case "/missing":
			http.Error(w, "missing", 404)
		case "/badbody":
			conn, b, _ := w.(http.Hijacker).Hijack()
			b.WriteString("HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: 9999\r\n\r\nshort")
			b.Flush()
			conn.Close()
		case "/timeout":
			<-r.Context().Done()
		default:
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<title>Page</title>")
		}
	}))
	defer proxy.Close()
	options := Options{DataDirectory: t.TempDir()}
	w, e := NewWorkspace(context.Background(), options)
	require.NoError(t, e)
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://a.report.test/root"}
	d.Scope = "open_web"
	d.Proxy.Mode = "http"
	d.Proxy.URL = proxy.URL
	d.Network.RequestTimeout = "300ms"
	zeroRetries := 0
	d.Network.MaxRetries = &zeroRetries
	d.RequestDelay = "0s"
	d.Termination.WhenDrained = true
	d.Fetch.Concurrency = 8
	record := createWorkspaceCrawl(t, w, d)
	id := record["id"].(string)
	startWorkspaceCrawl(t, w, record)
	done := waitWorkspace(t, w, id, func(r map[string]interface{}) bool { return latestRun(r)["state"] == "completed" })
	response := workspaceCall(t, w, "report", map[string]interface{}{"crawl_id": id}).(map[string]interface{})
	counts := response["counts"].(map[string]interface{})
	require.EqualValues(t, 9, counts["known"])
	require.EqualValues(t, 7, counts["visited"])
	require.EqualValues(t, 2, counts["not_visited"])
	require.False(t, counts["partial"].(bool))
	require.Equal(t, "average", response["rate_basis"])
	graph := workspaceCall(t, w, "host_graph", map[string]interface{}{"crawl_id": id, "host": "a.report.test", "depth": 2}).(map[string]interface{})
	require.Len(t, graph["nodes"], 4)
	require.Len(t, graph["edges"], 4)
	runID := latestRun(done)["id"].(string)
	path := filepath.Join(w.runRoot(id, runID), "state.json.report.sqlite")
	require.FileExists(t, path)
	require.NoError(t, w.Close())
	w, e = NewWorkspace(context.Background(), options)
	require.NoError(t, e)
	defer w.Close()
	restored := workspaceCall(t, w, "report", map[string]interface{}{"crawl_id": id, "run_id": runID}).(map[string]interface{})
	require.Equal(t, counts, restored["counts"])
	// Remove only the new reporting database to reproduce an older run. Retained
	// frontier and JSONL data must backfill without changing crawl state.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Remove(path + suffix)
	}
	workspaceCall(t, w, "report", map[string]interface{}{"crawl_id": id})
	deadline := time.Now().Add(10 * time.Second)
	var imported map[string]interface{}
	for time.Now().Before(deadline) {
		imported = workspaceCall(t, w, "report", map[string]interface{}{"crawl_id": id}).(map[string]interface{})
		if !imported["backfill_pending"].(bool) {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	require.False(t, imported["backfill_pending"].(bool))
	require.True(t, imported["counts"].(map[string]interface{})["partial"].(bool))
	// A failed redirect target exists only in the new receipt ledger; legacy
	// output cannot reconstruct it and must remain explicitly partial.
	require.EqualValues(t, 8, imported["counts"].(map[string]interface{})["known"])
	current := workspaceCall(t, w, "detail", map[string]interface{}{"crawl_id": id}).(map[string]interface{})
	workspaceCall(t, w, "delete", map[string]interface{}{"crawl_id": id, "revision": current["revision"], "confirm": true, "preserve_logs": true})
	waitDeleted(t, w, id)
	require.NoFileExists(t, path)
}

func TestReportRestartAndBackfillPreserveRunIdentityAndDiscoveries(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<title>Page</title>")
	}))
	defer proxy.Close()
	options := Options{DataDirectory: t.TempDir()}
	w, e := NewWorkspace(context.Background(), options)
	require.NoError(t, e)
	defer w.Close()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://old.report.test/"}
	d.Proxy.Mode = "http"
	d.Proxy.URL = proxy.URL
	no := false
	d.Network.RespectRobots = &no
	d.Termination.WhenDrained = true
	d.RequestDelay = "0s"
	record := createWorkspaceCrawl(t, w, d)
	id := record["id"].(string)
	workspaceCall(t, w, "start_run", map[string]interface{}{"crawl_id": id, "revision": record["revision"]})
	old := waitWorkspace(t, w, id, func(v map[string]interface{}) bool {
		return len(v["runs"].([]interface{})) == 1 && latestRun(v)["state"] == "completed"
	})
	oldRun := latestRun(old)["id"].(string)
	d.Seeds = []string{"http://new.report.test/"}
	d.Termination.WhenDrained = false
	draft := workspaceCall(t, w, "draft", map[string]interface{}{"crawl_id": id, "revision": old["revision"], "definition": d}).(map[string]interface{})
	startWorkspaceCrawl(t, w, draft)
	waitWorkspace(t, w, id, func(v map[string]interface{}) bool {
		runtime, _ := v["runtime"].(map[string]interface{})
		frame, _ := runtime["runtime"].(map[string]interface{})
		fetch, _ := frame["fetch"].(map[string]interface{})
		totals, _ := fetch["totals"].(map[string]interface{})
		successes, _ := totals["successes"].(float64)
		return successes >= 2
	})
	workspaceCall(t, w, "command", map[string]interface{}{"crawl_id": id, "action": "stop", "id": "stop-before-legacy-import"})
	waitWorkspace(t, w, id, func(v map[string]interface{}) bool { return runtimeState(v) == "stopped" })
	require.NoError(t, w.Close())
	path := filepath.Join(w.runRoot(id, oldRun), "state.json.report.sqlite")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		require.NoError(t, removeIfExists(path+suffix))
	}
	w, e = NewWorkspace(context.Background(), options)
	require.NoError(t, e)
	defer w.Close()
	workspaceCall(t, w, "report", map[string]interface{}{"crawl_id": id, "run_id": oldRun})
	deadline := time.Now().Add(10 * time.Second)
	for {
		view := workspaceCall(t, w, "report", map[string]interface{}{"crawl_id": id, "run_id": oldRun}).(map[string]interface{})
		if !view["backfill_pending"].(bool) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("historical import did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
	hosts := workspaceCall(t, w, "hosts", map[string]interface{}{"crawl_id": id, "run_id": oldRun}).([]interface{})
	require.Len(t, hosts, 2)
	names := []string{}
	for _, host := range hosts {
		names = append(names, host.(map[string]interface{})["name"].(string))
	}
	require.Contains(t, names, "old.report.test")
	require.Contains(t, names, "new.report.test")
	current := workspaceCall(t, w, "detail", map[string]interface{}{"crawl_id": id}).(map[string]interface{})
	require.Equal(t, oldRun, latestRun(current)["id"])
}
func removeIfExists(path string) error {
	e := os.Remove(path)
	if os.IsNotExist(e) {
		return nil
	}
	return e
}
