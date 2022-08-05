package filter

import (
	"encoding/json"
	"io"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/rpc"
	log "github.com/sirupsen/logrus"
)

type StreamFilter interface {
	Perform(entities []*types.Indexable) ([]json.RawMessage, error)
}

type SearchFilter interface {
	Perform() (io.Reader, error)
	IsAggregate() bool
	Postprocess([]json.RawMessage) ([]json.RawMessage, error)
}

func GetStreamFilter(params rpc.FilterParams) StreamFilter {
	switch params.DataType {
	case types.ArticleIndexable:
		return NewArticleStreamFilter(params)
	case types.MetricIndexable:
		return NewMetricStreamFilter(params)
	case types.PageIndexable:
		return NewPageStreamFilter(params)
	}

	log.Panicf("unknown filter data type: %s", params.DataType)

	return nil
}

func GetSearchFilter(params rpc.FilterParams) SearchFilter {
	switch params.DataType {
	case types.ArticleIndexable:
		return NewArticleSearchFilter(params)
	case types.MetricIndexable:
		return NewMetricSearchFilter(params)
	case types.PageIndexable:
		return NewPageSearchFilter(params)
	}

	log.Panicf("unknown filter data type: %s", params.DataType)

	return nil
}
