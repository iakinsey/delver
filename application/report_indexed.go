package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/report"
	"github.com/iakinsey/delver/types/message"
	"io"
	"net/http"
	"strings"
	"time"
)

// Indexed history is queried only when documents carry this crawl/run identity.
// Scrolls are bounded and released; no Elasticsearch dependency is introduced for
// crawls using the default JSONL sink.
func (w *Workspace) backfillIndexed(s *report.Store, r *CrawlRecord, run CrawlRun) {
	if run.Expert == nil || !run.LogsScoped {
		return
	}
	for _, worker := range run.Expert.Workers {
		if worker.Type != "transformer" {
			continue
		}
		var p struct {
			Addresses []string `json:"search_addresses"`
			Enabled   []string `json:"enabled"`
		}
		if json.Unmarshal(worker.Parameters, &p) != nil {
			continue
		}
		enabled := false
		for _, name := range p.Enabled {
			if name == "composite" {
				enabled = true
			}
		}
		if !enabled || len(p.Addresses) == 0 {
			continue
		}
		key := "indexed:" + worker.Name
		if done, _ := s.Progress(w.ctx, key); done == "done" {
			continue
		}
		if e := w.importIndexed(s, p.Addresses[0], r.ID, run); e != nil {
			s.Checkpoint(w.ctx, "error", "Indexed history unavailable: "+e.Error())
			continue
		}
		s.Checkpoint(w.ctx, key, "done")
	}
}
func (w *Workspace) importIndexed(s *report.Store, address, crawlID string, run CrawlRun) error {
	base := strings.TrimRight(address, "/")
	client := &http.Client{Timeout: 15 * time.Second}
	query := func(key, value string) interface{} {
		return map[string]interface{}{"bool": map[string]interface{}{"minimum_should_match": 1, "should": []interface{}{map[string]interface{}{"term": map[string]string{key: value}}, map[string]interface{}{"term": map[string]string{key + ".keyword": value}}}}}
	}
	body := interface{}(map[string]interface{}{"size": 64, "sort": []string{"_doc"}, "_source": []string{"uri", "origin", "effective_uri", "http_code", "features.url", "scope_root", "depth", "hops"}, "query": map[string]interface{}{"bool": map[string]interface{}{"filter": []interface{}{query("delver_crawl_id", crawlID), query("delver_run_id", run.ID)}}}})
	path := "/composite/_search?scroll=1m"
	scroll := ""
	defer func() {
		if scroll != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			b, _ := json.Marshal(map[string]interface{}{"scroll_id": []string{scroll}})
			req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, base+"/_search/scroll", bytes.NewReader(b))
			req.Header.Set("Content-Type", "application/json")
			if response, e := client.Do(req); e == nil {
				response.Body.Close()
			}
		}
	}()
	for {
		b, e := json.Marshal(body)
		if e != nil {
			return e
		}
		req, e := http.NewRequestWithContext(w.ctx, http.MethodPost, base+path, bytes.NewReader(b))
		if e != nil {
			return e
		}
		req.Header.Set("Content-Type", "application/json")
		response, e := client.Do(req)
		if e != nil {
			return fmt.Errorf("request failed")
		}
		var page struct {
			Scroll string `json:"_scroll_id"`
			Hits   struct {
				Hits []struct {
					Source message.CompositeAnalysis `json:"_source"`
				} `json:"hits"`
			} `json:"hits"`
		}
		if response.StatusCode == 404 {
			response.Body.Close()
			return nil
		}
		if response.StatusCode >= 300 {
			response.Body.Close()
			return fmt.Errorf("HTTP %d", response.StatusCode)
		}
		e = json.NewDecoder(io.LimitReader(response.Body, 64<<20)).Decode(&page)
		response.Body.Close()
		if e != nil {
			return fmt.Errorf("invalid or oversized history response")
		}
		if page.Scroll != "" {
			scroll = page.Scroll
		}
		if len(page.Hits.Hits) == 0 {
			return nil
		}
		events := []report.Event{}
		for _, hit := range page.Hits.Hits {
			c := hit.Source
			events = append(events, report.Event{URL: report.URL{URI: c.URI, Origin: c.Origin}, Status: c.HTTPCode})
			baseURI := c.URI
			if c.EffectiveURI != "" && c.EffectiveURI != c.URI {
				baseURI = c.EffectiveURI
				if c.HTTPCode > 0 {
					events = append(events, report.Event{URL: report.URL{URI: baseURI, Origin: c.URI}, Status: c.HTTPCode})
				}
			}
			var links []string
			if c.LoadPermissive("url", &links) {
				for _, link := range links {
					uri, _, e := report.Normalize(resolveReportURL(link, baseURI))
					if e == nil && reportLinkAllowed(run.Definition, c, uri) {
						events = append(events, report.Event{URL: report.URL{URI: uri, Origin: baseURI}})
					}
				}
			}
		}
		if e = s.Import(w.ctx, events); e != nil {
			return e
		}
		if scroll == "" {
			return fmt.Errorf("missing history cursor")
		}
		path = "/_search/scroll"
		body = map[string]interface{}{"scroll": "1m", "scroll_id": scroll}
	}
}
