package application

import (
	"context"
	"encoding/json"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/gateway"
	"github.com/iakinsey/delver/protection"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLiveProtectionElasticsearchOutageAndRetry(t *testing.T) {
	address := strings.TrimSuffix(os.Getenv("DELVER_ES_URL"), "/")
	if address == "" {
		t.Skip("set DELVER_ES_URL to an isolated Elasticsearch instance")
	}
	var offline atomic.Bool
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if offline.Load() {
			http.Error(w, "synthetic outage", 503)
			return
		}
		request, e := http.NewRequestWithContext(r.Context(), r.Method, address+r.URL.RequestURI(), r.Body)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		request.Header = r.Header.Clone()
		response, e := http.DefaultClient.Do(request)
		if e != nil {
			http.Error(w, e.Error(), 502)
			return
		}
		defer response.Body.Close()
		for key, values := range response.Header {
			w.Header()[key] = values
		}
		w.WriteHeader(response.StatusCode)
		io.Copy(w, response.Body)
	}))
	defer relay.Close()
	workspace, e := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	require.NoError(t, e)
	defer workspace.Close()
	definition := DefaultCrawlDefinition()
	definition.Seeds = []string{"http://blocked.test/", "http://good.test/"}
	record := createWorkspaceCrawl(t, workspace, definition)
	id := record["id"].(string)
	run := CrawlRun{ID: "outage-run", Definition: definition, State: "stopped", Desired: "stopped", LogsScoped: true}
	pipeline, e := CompileCrawl(workspace.runRoot(id, run.ID), run.ID, definition)
	require.NoError(t, e)
	parameters, _ := json.Marshal(map[string]interface{}{"search_addresses": []string{relay.URL}, "enabled": []string{"composite"}})
	pipeline.Workers = append(pipeline.Workers, config.Worker{Name: "logs", Type: "transformer", Inbox: "results", Manager: "worker", Count: 1, Parameters: parameters})
	run.Expert = &pipeline
	workspace.mu.Lock()
	workspace.records[id].Runs = append(workspace.records[id].Runs, run)
	require.NoError(t, workspace.saveLocked())
	workspace.mu.Unlock()
	defer gateway.DeleteCrawlLogs(context.Background(), []string{address}, id)
	for _, name := range []string{"blocked", "good"} {
		raw, _ := json.Marshal(map[string]interface{}{"delver_crawl_id": id, "delver_run_id": run.ID, "uri": "http://" + name + ".test/"})
		req, e := http.NewRequest("PUT", address+"/composite/_doc/"+id+"-"+name+"?refresh=true", strings.NewReader(string(raw)))
		require.NoError(t, e)
		req.Header.Set("Content-Type", "application/json")
		res, e := http.DefaultClient.Do(req)
		require.NoError(t, e)
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		require.Less(t, res.StatusCode, 300)
	}
	offline.Store(true)
	incident, e := workspace.protection.Trigger("http://blocked.test/", "synthetic", id, run.ID, 0)
	require.ErrorIs(t, e, protection.ErrBlocked)
	require.Eventually(t, func() bool {
		v, e := workspace.protection.PrivateIncident(incident.ID)
		return e == nil && v.State == "failed"
	}, 15*time.Second, 20*time.Millisecond)
	failed, e := workspace.protection.PrivateIncident(incident.ID)
	require.NoError(t, e)
	require.NotEmpty(t, failed.Error)
	require.ErrorIs(t, workspace.protection.Check("http://blocked.test/"), protection.ErrBlocked)
	offline.Store(false)
	body, _ := json.Marshal(map[string]interface{}{"action": "retry", "id": incident.ID})
	_, e = workspace.HandleWorkspace(context.Background(), "protection", body)
	require.NoError(t, e)
	require.Eventually(t, func() bool {
		v, e := workspace.protection.PrivateIncident(incident.ID)
		return e == nil && (v.State == "completed" || v.State == "failed")
	}, 15*time.Second, 20*time.Millisecond)
	done, e := workspace.protection.PrivateIncident(incident.ID)
	require.NoError(t, e)
	require.Equal(t, "completed", done.State, done.Error)
	for name, status := range map[string]int{"blocked": 404, "good": 200} {
		res, e := http.Get(address + "/composite/_doc/" + id + "-" + name)
		require.NoError(t, e)
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		require.Equal(t, status, res.StatusCode)
	}
}
