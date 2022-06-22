package types

type Indexable struct {
	ID    string
	Index string
	Streamable bool
	Data  interface{}
}

type Index struct {
	Name string
	Spec string
}

var Indices = []Index{
	{
		Name: "article",
		Spec: `{
			"settings":{},
			"mappings": {
				"properties": {
					"summary": {"type": "text"},
					"content": {"type": "text"},
					"title": {"type": "text"},
					"url": {"type": "keyword"},
					"url_md5": {"type": "keyword"},
					"origin_url": {"type": "keyword"},
					"type": {"type": "keyword"},
					"found": {"type": "date"},
					"binary_sentiment_naive_bayes_summary": {"type": "integer"},
					"binary_sentiment_naive_bayes_content": {"type": "integer"},
					"binary_sentiment_naive_bayes_title": {"type": "integer"},
					"binary_sentiment_naive_bayes_aggregate": {"type": "integer"},
					"countries": {
						"type": "text",
						"position_increment_gap": 100
					},
					"ngrams": {
						"type": "text",
						"position_increment_gap": 100
					},
					"corporate": {
						"type": "text",
						"position_increment_gap": 100
					}
				}
			}
		}`,
	},
	{
		Name: "page",
		Spec: `{
			"settings":{},
			"mappings": {
				"properties": {
					"uri": {"type": "keyword"},
					"host": {"type": "keyword"},
					"origin": {"type": "keyword"},
					"protocol": {"type": "keyword"},
					"content_md5": {"type": "keyword"},
					"elapsed_time_ms": {"type": "keyword"},
					"error": {"type": "text"},
					"timestamp": {"type": "integer"},
					"http_code": {"type": "integer"},
					"text": {"type": "text"}
				}
			}
		}`,
	},
	{
		Name: "metric",
		Spec: `{
			"settings":{},
			"mappings": {
				"properties": {
					"when": {"type": "keyword"},
					"value": {"type": "date"}
				}
			}
		}`,
	},
}
