package filter

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/rpc"
	"github.com/pkg/errors"
)

type metricSearchFilter struct {
	rpc.FilterParams
	rpc.MetricFilterQuery
}

type metricRollup struct {
	Value int64 `json:"value"`
}

type aggEntity struct {
	Key          int64        `json:"key"`
	MetricRollup metricRollup `json:"metric_rollup"`
}

func NewMetricSearchFilter(params rpc.FilterParams) SearchFilter {
	metricFilterQuery := params.Query.(rpc.MetricFilterQuery)

	return &metricSearchFilter{
		FilterParams:      params,
		MetricFilterQuery: metricFilterQuery,
	}
}

func (s *metricSearchFilter) IsAggregate() bool {
	return false
}

func (s *metricSearchFilter) Perform() (io.Reader, error) {
	aggType, err := s.getAggType(s.Agg.Name)

	if err != nil {
		return nil, err
	}

	// XXX Hardcode 90 day window limit
	if s.Start < time.Now().AddDate(0, 0, 90).Unix() {
		return nil, fmt.Errorf("start time exceeds 90 days from current date")
	}

	start := s.Start
	end := s.End

	if end == 0 {
		end = time.Now().Unix()
	}

	if end <= start {
		return nil, fmt.Errorf("end time before start time")
	}

	window := s.Agg.TimeWindowSeconds

	if window <= 0 {
		window = 1
	}

	interval := fmt.Sprintf("%dm", window)
	query := fmt.Sprintf(
		metricQueryTemplate,
		s.Key,
		start,
		end,
		interval,
		aggType,
	)

	return strings.NewReader(query), nil
}

func (s *metricSearchFilter) Postprocess(entities []json.RawMessage) (results []json.RawMessage, err error) {
	for _, entity := range entities {
		aggE := aggEntity{}

		if err = json.Unmarshal(entity, &aggE); err != nil {
			return nil, errors.Wrap(err, "failed to parse agg entity")
		}

		metric := types.Metric{
			Key:   s.Key,
			When:  aggE.Key / 1000, // Result is unix millis, convert to unix seconds
			Value: aggE.MetricRollup.Value,
		}

		b, err := json.Marshal(metric)

		if err != nil {
			return nil, errors.Wrap(err, "failed to serialize agg entity")
		}

		results = append(results, b)
	}

	return
}

func (s *metricSearchFilter) getAggType(name string) (string, error) {
	switch name {
	case "":
	case "sum":
		return "sum", nil
	case "mean":
		return "mean", nil
	}

	return "", fmt.Errorf("unsupported metric agg type: %s", name)
}
