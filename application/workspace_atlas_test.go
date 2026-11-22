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

func TestWorkspaceAtlasLifecycleAndStoredSnapshot(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/html")
		fmt.Fprint(rw, `<a href="http://b.atlas.test/page">B</a>`)
	}))
	defer proxy.Close()
	options := Options{DataDirectory: t.TempDir()}
	w, e := NewWorkspace(context.Background(), options)
	require.NoError(t, e)
	defer func() { w.Close() }()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://a.atlas.test/root"}
	d.Scope = "open_web"
	d.Proxy.Mode = "http"
	d.Proxy.URL = proxy.URL
	no := false
	d.Network.RespectRobots = &no
	d.Termination.WhenDrained = true
	d.RequestDelay = "0s"
	r := createWorkspaceCrawl(t, w, d)
	id := r["id"].(string)
	raw, e := w.HandleCrawls(context.Background(), "atlas_generate", []byte(`{"crawl_id":"`+id+`"}`))
	require.Error(t, e)
	require.Nil(t, raw)
	startWorkspaceCrawl(t, w, r)
	waitWorkspace(t, w, id, func(v map[string]interface{}) bool { return latestRun(v)["state"] == "completed" })
	request := map[string]interface{}{"crawl_id": id}
	job := workspaceCall(t, w, "atlas_generate", request).(map[string]interface{})
	require.Equal(t, "running", job["state"])
	var status map[string]interface{}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status = workspaceCall(t, w, "atlas_status", request).(map[string]interface{})
		if status["job"].(map[string]interface{})["state"] != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.Equal(t, "completed", status["job"].(map[string]interface{})["state"])
	snap := status["snapshot"].(map[string]interface{})
	require.EqualValues(t, 2, snap["hosts"])
	require.NoError(t, w.Close())
	w, e = NewWorkspace(context.Background(), options)
	require.NoError(t, e)
	status = workspaceCall(t, w, "atlas_status", request).(map[string]interface{})
	require.Equal(t, snap["id"], status["snapshot"].(map[string]interface{})["id"])
	frame := workspaceCall(t, w, "atlas_view", map[string]interface{}{"crawl_id": id, "snapshot": snap["id"], "bounds": []float64{-1, -1, 2, 2}, "node_limit": 100, "edge_limit": 100}).(map[string]interface{})
	require.Len(t, frame["nodes"], 2)
	workspaceCall(t, w, "atlas_generate", request)
	require.Eventually(t, func() bool {
		st := workspaceCall(t, w, "atlas_status", request).(map[string]interface{})
		return st["job"].(map[string]interface{})["state"] == "completed"
	}, 5*time.Second, 10*time.Millisecond)
	current := workspaceCall(t, w, "detail", request).(map[string]interface{})
	artifact := filepath.Join(w.runRoot(id, latestRun(current)["id"].(string)), "state.json.report.sqlite.atlas.sqlite")
	_, e = os.Stat(artifact)
	require.NoError(t, e)
	_, e = deleteRequest(t, w, current, true)
	require.NoError(t, e)
	waitDeleted(t, w, id)
	_, e = os.Stat(artifact)
	require.True(t, os.IsNotExist(e))
	w.mu.Lock()
	remaining := len(w.atlasJobs)
	w.mu.Unlock()
	require.Zero(t, remaining)

}
