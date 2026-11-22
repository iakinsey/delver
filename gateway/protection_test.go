package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/iakinsey/delver/protection"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveProtectionPurgeIsolation(t *testing.T) {
	address := strings.TrimSuffix(os.Getenv("DELVER_ES_URL"), "/")
	if address == "" {
		t.Skip("set DELVER_ES_URL to an isolated Elasticsearch instance")
	}
	index := "delver-protection-" + uuid.NewString()
	client := &http.Client{Timeout: 10 * time.Second}
	rest := func(method, path, body string) []byte {
		t.Helper()
		req, e := http.NewRequest(method, address+path, strings.NewReader(body))
		require.NoError(t, e)
		req.Header.Set("Content-Type", "application/json")
		res, e := client.Do(req)
		require.NoError(t, e)
		defer res.Body.Close()
		raw, e := io.ReadAll(res.Body)
		require.NoError(t, e)
		require.Less(t, res.StatusCode, 300, string(raw))
		return raw
	}
	rest("PUT", "/"+index, `{"settings":{"number_of_replicas":0},"mappings":{"properties":{"delver_crawl_id":{"type":"keyword"},"delver_run_id":{"type":"keyword"},"uri":{"type":"keyword"}}}}`)
	defer rest("DELETE", "/"+index, "")
	for _, row := range []struct{ id, crawl, run, uri string }{
		{"bad", "crawl", "run", "http://a.blocked.test/path"},
		{"good", "crawl", "run", "http://good.test/path"},
		{"other-run", "crawl", "other", "http://blocked.test/path"},
		{"other-crawl", "other", "run", "http://blocked.test/path"},
	} {
		body, _ := json.Marshal(map[string]string{"delver_crawl_id": row.crawl, "delver_run_id": row.run, "uri": row.uri})
		rest("PUT", "/"+index+"/_doc/"+row.id+"?refresh=true", string(body))
	}

	rest("POST", "/"+index+"/_update/good?refresh=true", `{"doc":{"origin":"http://blocked.test/from","features":{"url":["http://blocked.test/old","http://good.test/link"],"text":"Unrelated retained text","adversarial":{"structure":{"uri":"http://blocked.test/old","signature":[9007199254740993]},"enumeration":false}}}}`)
	match := protection.NewMatcher([]protection.Entry{{Domain: true, Value: "blocked.test"}}, nil).Matches
	require.NoError(t, purgeMatchingLogsInIndices(context.Background(), []string{address}, []string{index}, "crawl", "run", match))
	require.NoError(t, purgeMatchingLogsInIndices(context.Background(), []string{address}, []string{index}, "crawl", "run", match))
	var count struct {
		Count int `json:"count"`
	}
	require.NoError(t, json.Unmarshal(rest("GET", "/"+index+"/_count", ""), &count))
	require.Equal(t, 3, count.Count)

	good := rest("GET", "/"+index+"/_doc/good", "")
	require.NotContains(t, string(good), "blocked.test")
	require.Contains(t, string(good), "Unrelated retained text")
	require.Contains(t, string(good), "good.test/link")
	for _, id := range []string{"good", "other-run", "other-crawl"} {
		var value map[string]interface{}
		require.NoError(t, json.Unmarshal(rest("GET", fmt.Sprintf("/%s/_doc/%s", index, id), ""), &value))
		require.Equal(t, true, value["found"])
	}
}
