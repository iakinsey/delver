package gateway

import (
	"context"
	"encoding/json"

	"github.com/iakinsey/delver/types"
	"github.com/stretchr/testify/require"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveCrawlLogDeletionIsolation(t *testing.T) {
	address := os.Getenv("DELVER_ES_URL")
	if address == "" {
		t.Skip("set DELVER_ES_URL for live Elasticsearch deletion test")
	}
	prefix := "delver-delete-" + string(types.NewV4())
	indices := []string{prefix + "-composite", prefix + "-metric"}
	client := &http.Client{Timeout: 10 * time.Second}
	for _, index := range indices {
		index := index
		t.Cleanup(func() {
			req, _ := http.NewRequest(http.MethodDelete, address+"/"+index, nil)
			res, err := client.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			defer res.Body.Close()
			if res.StatusCode >= 300 && res.StatusCode != 404 {
				t.Errorf("cleanup status %d", res.StatusCode)
			}
		})
		for _, owner := range []string{"crawl-one", "crawl-two", ""} {
			req, _ := http.NewRequest(http.MethodPut, address+"/"+index+"/_doc/"+"doc-"+owner+"?refresh=false", strings.NewReader(`{"delver_crawl_id":"`+owner+`","title":"retained"}`))
			req.Header.Set("Content-Type", "application/json")
			res, err := client.Do(req)
			require.NoError(t, err)
			res.Body.Close()
			require.Less(t, res.StatusCode, 300)
		}
	}
	require.NoError(t, deleteCrawlLogsInIndices(context.Background(), []string{address}, indices, "crawl-one"))
	for _, index := range indices {
		res, err := client.Get(address + "/" + index + "/_search")
		require.NoError(t, err)
		var response struct {
			Hits struct {
				Hits []struct {
					Source struct {
						Owner string `json:"delver_crawl_id"`
					} `json:"_source"`
				} `json:"hits"`
			} `json:"hits"`
		}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&response))
		res.Body.Close()
		require.Len(t, response.Hits.Hits, 2)
		for _, hit := range response.Hits.Hits {
			require.NotEqual(t, "crawl-one", hit.Source.Owner)
		}
	}
}
