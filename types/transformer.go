package types

import (
	"encoding/json"
	"fmt"
)

const (
	MetricIndexable    = "metric"
	CompositeIndexable = "composite"
)

type Indexable struct {
	CrawlID string `json:"-"`
	RunID   string `json:"-"`
	ID      string
	// TODO Index and DataType may be redundant, but perhaps not if
	// we want to house the same data different indices
	Index      string
	DataType   string
	Streamable bool
	Data       interface{}
}

type Index struct {
	Name string
	Spec string
}

type ClientStreamerMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type Metric struct {
	Samples int64   `json:"sample_count,omitempty"`
	Key     string  `json:"key"`
	When    int64   `json:"when"`
	Value   float64 `json:"value"`
}

var Indices = []Index{
	{
		Name: MetricIndexable,
		Spec: `{
			"settings":{},
			"mappings": {
				"properties": {
 "delver_crawl_id": {"type":"keyword"}, "delver_run_id": {"type":"keyword"},
					"key": {"type":"keyword"},
					"when": {"type": "date", "format": "epoch_second"},
					"value": {"type": "double"}
				}
			}
		}`,
	},
	{
		Name: CompositeIndexable,
		Spec: `{
			"settings": {},
			"mappings": {
			  "properties": {
 "delver_crawl_id": {"type":"keyword"}, "delver_run_id": {"type":"keyword"},
				"store_key": { "type": "keyword" },
				"content_md5": { "type": "keyword" },
				"elapsed_time_ms": { "type": "keyword" },
				"error": { "type": "text" },
				"http_code": { "type": "integer" },
				"success": { "type": "boolean" },
				"timestamp": { "type": "long" },
				"request_id": { "type": "keyword" },
				"uri": { "type": "keyword" },
				"host": { "type": "keyword" },
				"origin": { "type": "keyword" },
				"protocol": { "type": "keyword" },
				"depth": { "type": "integer" },
				"features": {
				  "type": "object",
				  "properties": {
					"country": { "type": "keyword" },
					"company_name": { "type": "keyword" },
					"adversarial": {
					  "properties": {
						"enumeration": { "type": "boolean" },
						"subdomain_explosion": { "type": "boolean" }
					  }
					},
					"language": {
					  "properties": {
						"confidence": { "type": "float" },
						"name": { "type": "keyword" }
					  }
					},
					"text": { "type": "text", "fields":{"literal":{"type":"wildcard"}} },
					"title": { "type": "text", "fields":{"literal":{"type":"wildcard"},"exact":{"type":"keyword"}} },
					"url": { "type": "keyword" }
				  }
				},
				"header": {
				  "type": "object",
				  "properties": {
				  }
				}
			  }
			}
		}`,
	},
}

// IndexDocument adds ownership without changing the typed data used by live subscriptions.
func (i *Indexable) IndexDocument() (interface{}, error) {
	if i.CrawlID == "" {
		return i.Data, nil
	}
	raw, err := json.Marshal(i.Data)
	if err != nil {
		return nil, err
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil || document == nil {
		return nil, fmt.Errorf("indexed crawl logs must be JSON objects")
	}
	document["delver_crawl_id"], _ = json.Marshal(i.CrawlID)
	document["delver_run_id"], _ = json.Marshal(i.RunID)
	return document, nil
}
