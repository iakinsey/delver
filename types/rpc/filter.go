package rpc

const FilterTypeArticle = "article"
const FilterTypePage = "page"
const FilterTypeMetric = "metric"

type Filter struct {
	DataType string `json:"data_type"`
}

type ArticleFilter struct {
	Fields []string           `json:"fields"`
	Range  int                `json:"range"`
	Query  ArticleFilterQuery `json:"query"`
}

type ArticleFilterQuery struct {
	Keyword []string `json:"keyword"`
	Country []string `json:"country"`
	Company []string `json:"company"`
}

type PageFilter struct {
	Fields []string        `json:"fields"`
	Range  int             `json:"range"`
	Query  PageFilterQuery `json:"query"`
}

type PageFilterQuery struct {
	Url      []string `json:"url"`
	Domain   []string `json:"domain"`
	HttpCode []int    `json:"http_code"`
	Title    []string `json:"title"`
	Language []string `json:"language"`
}

type MetricFilter struct {
}
