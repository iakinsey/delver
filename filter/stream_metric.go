package filter

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/rpc"
	"github.com/pkg/errors"
)

type metricStreamFilter struct {
	rpc.FilterParams
	rpc.MetricFilterQuery
}

func NewMetricStreamFilter(params rpc.FilterParams) StreamFilter {
	metricFilter := params.Query.(rpc.MetricFilterQuery)

	return &metricStreamFilter{
		FilterParams:      params,
		MetricFilterQuery: metricFilter,
	}
}

func (s *metricStreamFilter) Perform(entities []*types.Indexable) (results []json.RawMessage, err error) {
	for _, e := range entities {
		entity := e.Data.(types.Metric)

		if match, err := s.filterDate(entity); err != nil {
			return nil, errors.Wrap(err, "invalid date metric filter")
		} else if !match {
			continue
		}

		if !s.filterKey(entity) {
			continue
		}
	}

	return
}

func (s *metricStreamFilter) filterDate(entity types.Metric) (bool, error) {
	// XXX Hardcode 90 day window limit
	if s.Start < time.Now().AddDate(0, 0, 90).Unix() {
		return false, fmt.Errorf("start time exceeds 90 days from current date")
	}

	start := s.Start
	end := s.End

	if end == 0 {
		end = time.Now().Unix()
	}

	if end <= start {
		return false, fmt.Errorf("end time before start time")
	}

	return end >= entity.When && entity.When >= start, nil
}

func (s *metricStreamFilter) filterKey(entity types.Metric) bool {
	return entity.Key == s.Key
}
