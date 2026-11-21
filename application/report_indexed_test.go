package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/types"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iakinsey/delver/report"
	"github.com/stretchr/testify/require"
)

func TestReportIndexedHistoryScopeScrollAndReceipts(t *testing.T) {
	var requests, released atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid JSON request")
			w.WriteHeader(400)
			return
		}
		if r.Method == http.MethodDelete {
			released.Add(1)
			w.Write([]byte(`{}`))
			return
		}
		n := requests.Add(1)
		if n == 1 {
			encoded, _ := json.Marshal(body)
			if !strings.Contains(string(encoded), `"delver_crawl_id":"crawl"`) || !strings.Contains(string(encoded), `"delver_run_id":"run"`) {
				t.Error("missing run scope")
			}
			w.Write([]byte(`{"_scroll_id":"cursor","hits":{"hits":[{"_source":{"uri":"https://old.test/","effective_uri":"https://new.test/","http_code":404}},{"_source":{"uri":"https://failed.test/","http_code":0}}]}}`))
		} else {
			if body["scroll_id"] != "cursor" {
				t.Error("incorrect cursor")
			}
			w.Write([]byte(`{"_scroll_id":"cursor","hits":{"hits":[]}}`))
		}
	}))
	defer server.Close()
	s, e := report.Open(filepath.Join(t.TempDir(), "report.sqlite"), true)
	require.NoError(t, e)
	defer s.Close()
	w := &Workspace{ctx: context.Background()}
	run := CrawlRun{ID: "run", Definition: DefaultCrawlDefinition()}
	require.NoError(t, w.importIndexed(s, server.URL, "crawl", run))
	counts, e := s.Counts(context.Background())
	require.NoError(t, e)
	require.EqualValues(t, 3, counts.Known)
	require.EqualValues(t, 2, counts.Visited)
	require.EqualValues(t, 1, counts.Edges)
	require.EqualValues(t, 2, requests.Load())
	require.EqualValues(t, 1, released.Load())
	// Replaying indexed history is idempotent even after a process interruption.
	requests.Store(0)
	require.NoError(t, w.importIndexed(s, server.URL, "crawl", run))
	after, e := s.Counts(context.Background())
	require.NoError(t, e)
	require.Equal(t, counts, after)
}
func TestReportIndexedHistoryErrorsAreExplicit(t *testing.T) {
	for _, status := range []int{401, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			s, e := report.Open(filepath.Join(t.TempDir(), "report.sqlite"), true)
			require.NoError(t, e)
			defer s.Close()
			w := &Workspace{ctx: context.Background()}
			err := w.importIndexed(s, server.URL, "crawl", CrawlRun{ID: "run"})
			require.Error(t, err)
			require.Contains(t, err.Error(), "HTTP")
		})
	}
}

// Invoke with DELVER_ES_URL pointing to an isolated test Elasticsearch instance.
func TestLiveReportIndexedHistory(t *testing.T) {
	address := strings.TrimRight(os.Getenv("DELVER_ES_URL"), "/")
	if address == "" {
		t.Skip("set DELVER_ES_URL to an isolated Elasticsearch instance")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	call := func(method, path string, body interface{}) {
		t.Helper()
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		req, e := http.NewRequest(method, address+path, bytes.NewReader(data))
		require.NoError(t, e)
		req.Header.Set("Content-Type", "application/json")
		res, e := client.Do(req)
		require.NoError(t, e)
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		require.Less(t, res.StatusCode, 300, string(raw))
	}
	crawlID, runID := string(types.NewV4()), string(types.NewV4())
	for i := 0; i < 72; i++ {
		id := crawlID + fmt.Sprint(i)
		t.Cleanup(func() {
			req, _ := http.NewRequest(http.MethodDelete, address+"/composite/_doc/"+id, nil)
			if res, e := client.Do(req); e == nil {
				res.Body.Close()
			}
		})
		owner, run := crawlID, runID
		if i == 70 {
			owner = "other-crawl"
		}
		if i == 71 {
			run = "other-run"
		}
		status := 404
		if i%3 == 0 {
			status = 0
		}
		call(http.MethodPut, "/composite/_doc/"+id, map[string]interface{}{"uri": fmt.Sprintf("https://live%d.test/", i), "origin": "https://source.test/", "http_code": status, "delver_crawl_id": owner, "delver_run_id": run})
	}
	call(http.MethodPost, "/composite/_refresh", nil)
	s, e := report.Open(filepath.Join(t.TempDir(), "report.sqlite"), true)
	require.NoError(t, e)
	defer s.Close()
	w := &Workspace{ctx: context.Background()}
	require.NoError(t, w.importIndexed(s, address, crawlID, CrawlRun{ID: runID, Definition: DefaultCrawlDefinition()}))
	counts, e := s.Counts(context.Background())
	require.NoError(t, e)
	require.EqualValues(t, 70, counts.Known)
	require.EqualValues(t, 46, counts.Visited)
	require.EqualValues(t, 24, counts.NotVisited)
	require.EqualValues(t, 70, counts.Edges)
	require.NoError(t, w.importIndexed(s, address, crawlID, CrawlRun{ID: runID, Definition: DefaultCrawlDefinition()}))
	after, e := s.Counts(context.Background())
	require.NoError(t, e)
	require.Equal(t, counts, after)
}
