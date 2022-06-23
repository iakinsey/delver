package rpc

const FilterTypeArticle = "article"
const FilterTypePage = "page"
const FilterTypeMetric = "metric"

type Filter struct {
	DataType string `json:"data_type"`
}

type ArticleFilter struct{}

type PageFilter struct{}

type MetricFilter struct{}
