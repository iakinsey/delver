package application

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/gateway"
	"github.com/iakinsey/delver/types"
	"github.com/stretchr/testify/require"
)

func TestLiveWorkspaceLogDeletionOptions(t *testing.T) {
	address := strings.TrimSuffix(os.Getenv("DELVER_ES_URL"), "/")
	if address == "" {
		t.Skip("set DELVER_ES_URL to an isolated Elasticsearch instance")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	call := func(method, path string, body interface{}) int {
		t.Helper()
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		req, err := http.NewRequest(method, address+path, bytes.NewReader(data))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		require.NoError(t, err)
		defer res.Body.Close()
		io.Copy(io.Discard, res.Body)
		return res.StatusCode
	}
	for _, preserve := range []bool{true, false} {
		t.Run(map[bool]string{true: "preserve", false: "purge"}[preserve], func(t *testing.T) {
			w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
			require.NoError(t, err)
			defer w.Close()
			d := DefaultCrawlDefinition()
			d.Seeds = []string{"http://example.test/"}
			record := createWorkspaceCrawl(t, w, d)
			id := record["id"].(string)
			control := string(types.NewV4())
			defer func() {
				require.NoError(t, gateway.DeleteCrawlLogs(context.Background(), []string{address}, id))
				require.NoError(t, gateway.DeleteCrawlLogs(context.Background(), []string{address}, control))
			}()
			for _, owner := range []string{id, control} {
				status := call("PUT", "/composite/_doc/"+owner+"?refresh=true", map[string]interface{}{"delver_crawl_id": owner, "title": "synthetic log"})
				require.Contains(t, []int{200, 201}, status)
			}
			params, _ := json.Marshal(map[string]interface{}{"search_addresses": []string{address}, "enabled": []string{"composite"}})
			w.mu.Lock()
			w.records[id].Runs = []CrawlRun{{ID: "logs-run", LogsScoped: true, Expert: &config.Application{Workers: []config.Worker{{Type: "transformer", Parameters: params}}}}}
			w.mu.Unlock()
			_, err = deleteRequest(t, w, record, preserve)
			require.NoError(t, err)
			waitDeleted(t, w, id)
			expected := 404
			if preserve {
				expected = 200
			}
			require.Equal(t, expected, call("GET", "/composite/_doc/"+id, nil))
			require.Equal(t, 200, call("GET", "/composite/_doc/"+control, nil), "other crawl logs must remain")
		})
	}
}
