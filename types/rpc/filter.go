package rpc

import "encoding/json"

const FilterTypeArticle = "article"
const FilterTypePage = "page"
const FilterTypeMetric = "metric"

type Filter struct {
	DataType string `json:"data_type"`
}

type Aggregator struct {
	Name              string `json:"agg_name"`
	TimeField         string `json:"time_field"`
	AggField          string `json:"agg_field"`
	TimeWindowSeconds int32  `json:"time_window_seconds"`
}

type ArticleFilterQuery struct {
	Keyword []string `json:"keyword"`
	Country []string `json:"country"`
	Company []string `json:"company"`
}

type MetricFilterQuery struct{}

type PageFilterQuery struct {
	Url      []string `json:"url"`
	Domain   []string `json:"domain"`
	HttpCode []int    `json:"http_code"`
	Title    []string `json:"title"`
	Language []string `json:"language"`
}

type FilterParams struct {
	Fields   []string        `json:"fields"`
	Range    int             `json:"range"`
	RawQuery json.RawMessage `json:"query"`
	Options  map[string]bool `json:"options"`
	Agg      Aggregator      `json:"agg"`
	Query    interface{}
}
