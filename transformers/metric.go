package transformers

import (
	"encoding/json"

	"github.com/iakinsey/delver/types"
	"github.com/pkg/errors"
)

type metricTransformer struct{}

func NewMetricTransformer() Transformer {
	return &metricTransformer{}
}

func (s *metricTransformer) Perform(msg json.RawMessage) ([]*types.Indexable, error) {
	var metrics []types.Metric
	var results []*types.Indexable

	if err := json.Unmarshal(msg, &metrics); err != nil {
		return nil, errors.Wrap(err, "transformer failed to parse metrics")
	}

	for _, m := range metrics {
		results = append(results, &types.Indexable{
			ID:         string(types.NewV4()),
			Index:      types.MetricIndexable,
			DataType:   types.MetricIndexable,
			Data:       m,
			Streamable: s.Streamable(),
		})
	}
	return results, nil
}

func (s *metricTransformer) Input() types.MessageType {
	return types.MetricType
}

func (s *metricTransformer) Streamable() bool {
	return true
}
