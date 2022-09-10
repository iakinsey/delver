package rpc

import "encoding/json"

const FilterTypeArticle = "article"
const FilterTypePage = "page"
const FilterTypeMetric = "metric"

type Aggregator struct {
	Name              string `json:"agg_name"`
	TimeField         string `json:"time_field"`
	AggField          string `json:"agg_field"`
	TimeWindowSeconds int64  `json:"time_window_seconds"`
}

type ArticleFilterQuery struct {
	Keyword []string `json:"keyword"`
	Country []string `json:"country"`
	Company []string `json:"company"`
}

type MetricFilterQuery struct {
	Key   string `json:"key"`
	Start int64  `json:"start"`
	End   int64  `json:"end"`
}

type PageFilterQuery struct {
	Url      []string `json:"url"`
	Domain   []string `json:"domain"`
	HttpCode []int    `json:"http_code"`
	Title    []string `json:"title"`
	Language []string `json:"language"`
}

type FilterParams struct {
	Fields    []string        `json:"fields"`
	Range     int             `json:"range"`
	RawQuery  json.RawMessage `json:"query"`
	DataType  string          `json:"data_type"`
	Options   map[string]bool `json:"options"`
	Callback  string          `json:"callback"`
	Agg       *Aggregator     `json:"agg"`
	DoNothing bool
	Query     interface{}
}
