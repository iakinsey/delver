package application

import (
	"context"
	"encoding/json"
	"github.com/iakinsey/delver/config"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func deleteRequest(t *testing.T, w *Workspace, r map[string]interface{}, keep bool) (interface{}, error) {
	t.Helper()
	raw, _ := json.Marshal(map[string]interface{}{"crawl_id": r["id"], "revision": r["revision"], "confirm": true, "preserve_logs": keep})
	return w.HandleCrawls(context.Background(), "delete", raw)
}
func waitDeleted(t *testing.T, w *Workspace, id string) {
	t.Helper()
	require.Eventually(t, func() bool { w.mu.Lock(); defer w.mu.Unlock(); return w.records[id] == nil }, 5*time.Second, 10*time.Millisecond)
}
func TestDeleteCrawlRequiresConfirmationRemovesAllLocalHistoryAndSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://example.test/"}
	r := createWorkspaceCrawl(t, w, d)
	other := createWorkspaceCrawl(t, w, d)
	id := r["id"].(string)
	owned := filepath.Join(root, "crawls", id, "runs", "history")
	require.NoError(t, os.MkdirAll(owned, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(owned, "results.jsonl"), []byte("history"), 0600))
	raw, _ := json.Marshal(map[string]interface{}{"crawl_id": id, "revision": r["revision"]})
	_, err = w.HandleCrawls(context.Background(), "delete", raw)
	require.Error(t, err)
	require.Contains(t, err.Error(), "confirm")
	bad, _ := json.Marshal(map[string]interface{}{"crawl_id": id, "revision": 0, "confirm": true})
	_, err = w.HandleCrawls(context.Background(), "delete", bad)
	require.Error(t, err)
	require.Contains(t, err.Error(), "revision conflict")
	_, err = deleteRequest(t, w, r, true)
	require.NoError(t, err)
	waitDeleted(t, w, id)
	_, err = os.Stat(filepath.Join(root, "crawls", id))
	require.True(t, os.IsNotExist(err))
	require.NoError(t, w.Close())
	w, err = NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	defer w.Close()
	w.mu.Lock()
	require.Nil(t, w.records[id])
	require.NotNil(t, w.records[other["id"].(string)])
	w.mu.Unlock()
	_, err = deleteRequest(t, w, r, true)
	require.NoError(t, err)
}
func TestDeleteCrawlScopesElasticsearchAndPreservesOnRequest(t *testing.T) {
	for _, keep := range []bool{true, false} {
		t.Run(map[bool]string{true: "keep", false: "purge"}[keep], func(t *testing.T) {
			var calls atomic.Int32
			var expectedID string
			server := httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
				resp.Header().Set("X-Elastic-Product", "Elasticsearch")
				if req.URL.Path == "/" {
					resp.Write([]byte(`{"version":{"number":"7.17.13"},"tagline":"You Know, for Search"}`))
					return
				}
				if strings.HasSuffix(req.URL.Path, "/_refresh") {
					resp.Write([]byte(`{"_shards":{"failed":0}}`))
					return
				}
				require.Equal(t, http.MethodPost, req.Method)
				require.Equal(t, "/composite,metric/_delete_by_query", req.URL.Path)
				var body map[string]interface{}
				require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
				data, _ := json.Marshal(body)
				require.Contains(t, string(data), "delver_crawl_id")
				require.Contains(t, string(data), expectedID)
				require.NotContains(t, string(data), "match_all")
				calls.Add(1)
				resp.Write([]byte(`{"deleted":2,"failures":[],"timed_out":false}`))
			}))
			defer server.Close()
			w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
			require.NoError(t, err)
			defer w.Close()
			d := DefaultCrawlDefinition()
			d.Seeds = []string{"http://example.test/"}
			r := createWorkspaceCrawl(t, w, d)
			id := r["id"].(string)
			expectedID = id
			params, _ := json.Marshal(map[string]interface{}{"search_addresses": []string{server.URL}, "enabled": []string{"composite", "metric"}})
			w.mu.Lock()
			w.records[id].Runs = []CrawlRun{{ID: "test-run", LogsScoped: true, Expert: &config.Application{Workers: []config.Worker{{Type: "transformer", Parameters: params}}}}}
			w.mu.Unlock()
			_, err = deleteRequest(t, w, r, keep)
			require.NoError(t, err)
			waitDeleted(t, w, id)
			if keep {
				require.Equal(t, int32(0), calls.Load())
			} else {
				require.Equal(t, int32(1), calls.Load())
			}
		})
	}
}
func TestDeleteCrawlRejectsSharedStorageAndUnattributedLogs(t *testing.T) {
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	require.NoError(t, err)
	defer w.Close()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://example.test/"}
	r := createWorkspaceCrawl(t, w, d)
	id := r["id"].(string)
	w.mu.Lock()
	w.records[id].Runs = []CrawlRun{{ID: "legacy", Expert: &config.Application{Workers: []config.Worker{{Type: "transformer", Parameters: json.RawMessage(`{"enabled":["composite"]}`)}}}}}
	w.mu.Unlock()
	_, err = deleteRequest(t, w, r, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "preserve_logs=true")
	w.mu.Lock()
	w.records[id].Runs = []CrawlRun{{Legacy: true, Expert: &config.Application{Resources: []config.Resource{{Parameters: json.RawMessage(`{"path":"` + strings.ReplaceAll(w.root, "\\", "\\\\") + `"}`)}}}}}
	w.mu.Unlock()
	_, err = deleteRequest(t, w, r, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "overlaps workspace")
}

func TestDeleteStopsRunningCrawlAndDoesNotResurrectIt(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(404)
			return
		}
		w.Write([]byte("<title>page</title>"))
	}))
	defer site.Close()
	root := t.TempDir()
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	d := DefaultCrawlDefinition()
	d.Seeds = []string{site.URL}
	r := createWorkspaceCrawl(t, w, d)
	startWorkspaceCrawl(t, w, r)
	current := workspaceCall(t, w, "detail", map[string]interface{}{"crawl_id": r["id"]}).(map[string]interface{})
	w.mu.Lock()
	slot := w.slots[r["id"].(string)]
	require.NotNil(t, slot.supervisor)
	require.Equal(t, r["id"], slot.supervisor.crawlID)
	w.mu.Unlock()
	_, err = deleteRequest(t, w, current, true)
	require.NoError(t, err)
	waitDeleted(t, w, r["id"].(string))
	require.NoError(t, w.Close())
	w, err = NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	defer w.Close()
	require.Len(t, w.records, 0)
}
func TestFailedElasticsearchDeleteRetainsCrawlAndCanRetry(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		if r.URL.Path == "/" {
			w.Write([]byte(`{"version":{"number":"7.17.13"},"tagline":"You Know, for Search"}`))
			return
		}
		if fail.Load() {
			w.WriteHeader(503)
			w.Write([]byte(`{"error":"unavailable"}`))
			return
		}
		w.Write([]byte(`{"failures":[]}`))
	}))
	defer server.Close()
	root := t.TempDir()
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://example.test/"}
	r := createWorkspaceCrawl(t, w, d)
	id := r["id"].(string)
	params, _ := json.Marshal(map[string]interface{}{"search_addresses": []string{server.URL}, "enabled": []string{"composite"}})
	w.mu.Lock()
	w.records[id].Runs = []CrawlRun{{ID: "old-run", Definition: d, LogsScoped: true, Expert: &config.Application{Workers: []config.Worker{{Type: "transformer", Parameters: params}}}}}
	w.mu.Unlock()
	data := filepath.Join(root, "crawls", id)
	require.NoError(t, os.MkdirAll(data, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(data, "saved"), []byte("data"), 0600))
	_, err = deleteRequest(t, w, r, false)
	require.NoError(t, err)
	require.Eventually(t, func() bool { w.mu.Lock(); defer w.mu.Unlock(); return !activeWorkflow(w.records[id]) }, 8*time.Second, 10*time.Millisecond)
	_, err = os.Stat(filepath.Join(data, "saved"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	w, err = NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	defer w.Close()
	require.Nil(t, w.slots[id].supervisor)
	fail.Store(false)
	_, err = deleteRequest(t, w, r, false)
	require.NoError(t, err)
	waitDeleted(t, w, id)
}
