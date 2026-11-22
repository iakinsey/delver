package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/elastic/go-elasticsearch/v7"
	"github.com/iakinsey/delver/protection"
	"time"
)

// PurgeMatchingLogs scans only explicitly owned documents and reads location
// fields, not extracted content. It never deletes an entire shared index.
func PurgeMatchingLogs(ctx context.Context, addresses []string, crawlID, runID string, match func(string) bool) error {
	return purgeMatchingLogsInIndices(ctx, addresses, []string{"composite", "metric"}, crawlID, runID, match)
}
func purgeMatchingLogsInIndices(ctx context.Context, addresses, indices []string, crawlID, runID string, match func(string) bool) error {
	if crawlID == "" || runID == "" {
		return fmt.Errorf("crawl and run identities are required")
	}
	if e := scanProtectionLogs(ctx, addresses, indices, crawlID, runID, match, true); e != nil {
		return e
	}
	return scanProtectionLogs(ctx, addresses, indices, crawlID, runID, match, false)
}
func scanProtectionLogs(ctx context.Context, addresses, indices []string, crawlID, runID string, match func(string) bool, remove bool) error {
	client, e := elasticsearch.NewClient(elasticsearch.Config{Addresses: addresses})
	if e != nil {
		return e
	}
	ref, e := client.Indices.Refresh(client.Indices.Refresh.WithContext(ctx), client.Indices.Refresh.WithIndex(indices...), client.Indices.Refresh.WithIgnoreUnavailable(true))
	if e != nil {
		return e
	}
	if ref.IsError() {
		ref.Body.Close()
		return fmt.Errorf("Elasticsearch refresh: HTTP %d", ref.StatusCode)
	}
	var refresh struct {
		Shards struct {
			Failed int `json:"failed"`
		} `json:"_shards"`
	}
	e = json.NewDecoder(ref.Body).Decode(&refresh)
	ref.Body.Close()
	if e != nil {
		return e
	}
	if refresh.Shards.Failed > 0 {
		return fmt.Errorf("Elasticsearch refresh incomplete")
	}
	query := map[string]interface{}{"size": 512, "_source": []string{"uri", "effective_uri", "origin", "host", "error", "header", "features.url", "features.adversarial"}, "sort": []string{"_doc"}, "query": map[string]interface{}{"bool": map[string]interface{}{"filter": []interface{}{protectionOwnerQuery("delver_crawl_id", crawlID), protectionOwnerQuery("delver_run_id", runID)}}}}
	body, _ := json.Marshal(query)
	res, e := client.Search(client.Search.WithContext(ctx), client.Search.WithIndex(indices...), client.Search.WithBody(bytes.NewReader(body)), client.Search.WithScroll(time.Minute), client.Search.WithIgnoreUnavailable(true))
	if e != nil {
		return e
	}
	scroll := ""
	defer func() {
		if scroll != "" {
			c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			r, e := client.ClearScroll(client.ClearScroll.WithContext(c), client.ClearScroll.WithScrollID(scroll))
			if e == nil {
				r.Body.Close()
			}
		}
	}()
	for {
		if res.IsError() {
			code := res.StatusCode
			res.Body.Close()
			return fmt.Errorf("Elasticsearch purge search: HTTP %d", code)
		}
		var page struct {
			Scroll   string `json:"_scroll_id"`
			TimedOut bool   `json:"timed_out"`
			Shards   struct {
				Failed int `json:"failed"`
			} `json:"_shards"`
			Hits struct {
				Rows []struct {
					ID     string          `json:"_id"`
					Index  string          `json:"_index"`
					Source json.RawMessage `json:"_source"`
				} `json:"hits"`
			} `json:"hits"`
		}
		e = json.NewDecoder(res.Body).Decode(&page)
		res.Body.Close()
		if e != nil {
			return e
		}
		scroll = page.Scroll
		if page.TimedOut || page.Shards.Failed != 0 {
			return fmt.Errorf("Elasticsearch purge search incomplete")
		}
		if len(page.Hits.Rows) == 0 {
			break
		}
		var deletes bytes.Buffer

		for _, hit := range page.Hits.Rows {
			clean, changed, e := protection.SanitizeJSON(hit.Source, match)
			if e != nil {
				return e
			}
			if !changed {
				continue
			}
			if !remove {
				return fmt.Errorf("matching Elasticsearch document or reference remains")
			}
			action := "delete"
			if clean != nil {
				action = "update"
			}
			line, _ := json.Marshal(map[string]interface{}{action: map[string]string{"_index": hit.Index, "_id": hit.ID}})
			deletes.Write(line)
			deletes.WriteByte('\n')
			if clean != nil {
				doc, e := nullRemovedFields(hit.Source, clean)
				if e != nil {
					return e
				}
				line, e = json.Marshal(map[string]json.RawMessage{"doc": doc})
				if e != nil {
					return e
				}
				deletes.Write(line)
				deletes.WriteByte('\n')
			}
		}

		if deletes.Len() > 0 {
			r, e := client.Bulk(&deletes, client.Bulk.WithContext(ctx), client.Bulk.WithRefresh("wait_for"))
			if e != nil {
				return e
			}
			var result struct {
				Errors bool `json:"errors"`
			}
			e = json.NewDecoder(r.Body).Decode(&result)
			bad := r.IsError()
			r.Body.Close()
			if e != nil {
				return e
			}
			if bad || result.Errors {
				return fmt.Errorf("Elasticsearch purge delete incomplete")
			}
		}
		if scroll == "" {
			return fmt.Errorf("Elasticsearch did not return a purge scroll")
		}
		res, e = client.Scroll(client.Scroll.WithContext(ctx), client.Scroll.WithScrollID(scroll), client.Scroll.WithScroll(time.Minute))
		if e != nil {
			return e
		}
	}
	return nil
}

// Elasticsearch updates merge objects recursively. Explicit nulls replace
// removed fields while preserving fields that were never fetched for cleanup.
func nullRemovedFields(original, clean json.RawMessage) (json.RawMessage, error) {
	var before, after map[string]json.RawMessage
	if json.Unmarshal(original, &before) != nil || json.Unmarshal(clean, &after) != nil || before == nil || after == nil {
		return clean, nil
	}
	for key, value := range before {
		next, ok := after[key]
		if !ok {
			after[key] = json.RawMessage("null")
			continue
		}
		merged, e := nullRemovedFields(value, next)
		if e != nil {
			return nil, e
		}
		after[key] = merged
	}
	return json.Marshal(after)
}

// Old dynamically mapped indices expose ownership as text plus a keyword field.
// Exact keyword matching also supports explicitly keyword-mapped newer indices.
func protectionOwnerQuery(field, value string) interface{} {
	return map[string]interface{}{"bool": map[string]interface{}{"minimum_should_match": 1, "should": []interface{}{
		map[string]interface{}{"term": map[string]string{field: value}},
		map[string]interface{}{"term": map[string]string{field + ".keyword": value}},
	}}}
}
