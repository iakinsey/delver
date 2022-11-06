package types

import "encoding/json"

const (
	ArticleIndexable = "article"
	MetricIndexable  = "metric"
	PageIndexable    = "page"
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

type Article struct {
	Summary                            string   `json:"summary"`
	Content                            string   `json:"content"`
	Title                              string   `json:"title"`
	Url                                string   `json:"url"`
	UrlMd5                             string   `json:"url_md5"`
	OriginUrl                          string   `json:"origin_url"`
	Type                               string   `json:"type"`
	Found                              int64    `json:"found"`
	BinarySentimentNaiveBayesSummary   int      `json:"binary_sentiment_naive_bayes_summary"`
	BinarySentimentNaiveBayesContent   int      `json:"binary_sentiment_naive_bayes_content"`
	BinarySentimentNaiveBayesTitle     int      `json:"binary_sentiment_naive_bayes_title"`
	BinarySentimentNaiveBayesAggregate int      `json:"binary_sentiment_naive_bayes_aggregate"`
	Countries                          []string `json:"countries"`
	Ngrams                             []string `json:"ngrams"`
	Corporate                          []string `json:"corporate"`
}

type Page struct {
	Uri           string `json:"uri"`
	Host          string `json:"host"`
	Origin        string `json:"origin"`
	Protocol      string `json:"protocol"`
	ContentMd5    string `json:"content_md5"`
	ElapsedTimeMs int64  `json:"elapsed_time_ms"`
	Error         string `json:"error"`
	Timestamp     int64  `json:"timestamp"`
	HttpCode      int    `json:"http_code"`
	Text          string `json:"text"`
	Language      string `json:"language"`
	Title         string `json:"title"`
}

type Metric struct {
	Key   string `json:"key"`
	When  int64  `json:"when"`
	Value int64  `json:"value"`
}

var Indices = []Index{
	{
		Name: ArticleIndexable,
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
					"found": {"type": "date", "format": "epoch_second"},
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
		Name: PageIndexable,
		Spec: `{
			"settings":{},
			"mappings": {
				"properties": {
					"uri": {"type": "keyword"},
					"host": {"type": "keyword"},
					"origin": {"type": "keyword"},
					"protocol": {"type": "keyword"},
					"content_md5": {"type": "keyword"},
					"language": {"type": "keyword"},
					"error": {"type": "text"},
					"title": {"type": "text"},
					"elapsed_time_ms": {"type": "integer"},
					"timestamp": {"type": "integer"},
					"http_code": {"type": "integer"}
				}
			}
		}`,
	},
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
}
