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
	Value float64 `json:"value"`
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
	return true
}

func (s *metricSearchFilter) Perform() (io.Reader, error) {
	aggType, err := s.getAggType()

	if err != nil {
		return nil, err
	}

	start := s.Start
	ninetyDaysAgo := time.Now().AddDate(0, 0, -90).Unix()

	if start == 0 {
		start = ninetyDaysAgo
	} else if start < ninetyDaysAgo {
		// XXX Hardcode 90 day window limit
		return nil, fmt.Errorf("start time exceeds 90 days from current date")
	}

	end := s.End

	if end == 0 {
		end = time.Now().Unix()
	}

	if end <= start {
		return nil, fmt.Errorf("end time before start time")
	}

	window := 1

	if s.Agg != nil && s.Agg.TimeWindowSeconds > 0 {
		window = int(s.Agg.TimeWindowSeconds)
	}

	interval := fmt.Sprintf("%dm", window)
	query := fmt.Sprintf(
		metricQueryTemplate,
		s.Key,
		start*1000,
		end*1000,
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
			Value: int64(aggE.MetricRollup.Value),
		}

		b, err := json.Marshal(metric)

		if err != nil {
			return nil, errors.Wrap(err, "failed to serialize agg entity")
		}

		results = append(results, b)
	}

	return
}

func (s *metricSearchFilter) getAggType() (string, error) {
	if s.Agg == nil {
		return "sum", nil
	}

	switch s.Agg.Name {
	case "":
	case "sum":
		return "sum", nil
	case "mean":
		return "mean", nil
	}

	return "", fmt.Errorf("unsupported metric agg type: %s", s.Agg.Name)
}
