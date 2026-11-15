package application

import (
	"encoding/json"
	"github.com/iakinsey/delver/config"
	"time"
)

type SourceSummary struct {
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	Interval string      `json:"interval"`
	URIs     interface{} `json:"uris"`
}

func sourceSummaries(app config.Application) []SourceSummary {
	sources := []SourceSummary{}
	for _, worker := range app.Workers {
		if worker.Type != "fixed_seed_publisher" && worker.Type != "rss_feed_publisher" && worker.Type != "dfs_basic_publisher" {
			continue
		}
		var parameters map[string]interface{}
		json.Unmarshal(worker.Parameters, &parameters)
		sources = append(sources, SourceSummary{Name: worker.Name, Type: worker.Type, Interval: time.Duration(worker.Interval).String(), URIs: redactValue(parameters["uris"])})
	}
	return sources
}
func crawlIdentities(app config.Application) map[string]string {
	identities := map[string]string{}
	for _, resource := range app.Resources {
		if resource.Type == "crawl_queue" {
			var p struct {
				CrawlID string `json:"crawl_id"`
			}
			if json.Unmarshal(resource.Parameters, &p) == nil {
				identities[resource.Name] = p.CrawlID
			}
		}
	}
	return identities
}
