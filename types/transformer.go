package types

import "encoding/json"

const (
	MetricIndexable    = "metric"
	CompositeIndexable = "composite"
)

type Indexable struct {
	ID string
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
	Key   string `json:"key"`
	When  int64  `json:"when"`
	Value int64  `json:"value"`
}

var Indices = []Index{
	{
		Name: MetricIndexable,
		Spec: `{
			"settings":{},
			"mappings": {
				"properties": {
					"key": {"type":"keyword"},
					"when": {"type": "date", "format": "epoch_second"},
					"value": {"type": "integer"}
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
					"text": { "type": "text" },
					"title": { "type": "text" },
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
