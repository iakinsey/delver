package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/elastic/go-elasticsearch/v7"
)

// DeleteCrawlLogs removes only documents explicitly attributed to this crawl.
// Never delete shared indices or infer ownership from hosts or timestamps.
func DeleteCrawlLogs(ctx context.Context, addresses []string, crawlID string) error {
	return deleteCrawlLogsInIndices(ctx, addresses, []string{"composite", "metric"}, crawlID)
}
func deleteCrawlLogsInIndices(ctx context.Context, addresses, indices []string, crawlID string) error {
	if crawlID == "" {
		return fmt.Errorf("crawl identity is required")
	}
	client, err := elasticsearch.NewClient(elasticsearch.Config{Addresses: addresses})
	if err != nil {
		return err
	}
	// Include recently indexed documents even when Elasticsearch's periodic
	// refresh has not run yet.
	refreshed, err := client.Indices.Refresh(client.Indices.Refresh.WithIndex(indices...), client.Indices.Refresh.WithContext(ctx), client.Indices.Refresh.WithIgnoreUnavailable(true), client.Indices.Refresh.WithAllowNoIndices(true))
	if err != nil {
		return err
	}
	if refreshed.IsError() {
		refreshed.Body.Close()
		return fmt.Errorf("Elasticsearch refresh failed: HTTP %d", refreshed.StatusCode)
	}
	var refreshResult struct {
		Shards struct {
			Failed int `json:"failed"`
		} `json:"_shards"`
	}
	err = json.NewDecoder(refreshed.Body).Decode(&refreshResult)
	refreshed.Body.Close()
	if err != nil {
		return err
	}
	if refreshResult.Shards.Failed > 0 {
		return fmt.Errorf("Elasticsearch could not refresh all crawl log shards")
	}
	body, _ := json.Marshal(map[string]interface{}{"query": map[string]interface{}{"bool": map[string]interface{}{
		"minimum_should_match": 1, "should": []interface{}{
			map[string]interface{}{"term": map[string]string{"delver_crawl_id": crawlID}},
			map[string]interface{}{"term": map[string]string{"delver_crawl_id.keyword": crawlID}},
		},
	}}})
	res, err := client.DeleteByQuery(indices, bytes.NewReader(body), client.DeleteByQuery.WithContext(ctx), client.DeleteByQuery.WithIgnoreUnavailable(true), client.DeleteByQuery.WithAllowNoIndices(true), client.DeleteByQuery.WithRefresh(true))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("Elasticsearch log deletion failed: HTTP %d", res.StatusCode)
	}
	var result struct {
		TimedOut  bool              `json:"timed_out"`
		Failures  []json.RawMessage `json:"failures"`
		Conflicts int               `json:"version_conflicts"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return err
	}
	if result.TimedOut || len(result.Failures) > 0 || result.Conflicts > 0 {
		return fmt.Errorf("Elasticsearch did not finish deleting crawl logs; retry deletion")
	}
	return nil
}
