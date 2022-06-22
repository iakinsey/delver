package transformers

import (
	"encoding/json"

	"github.com/iakinsey/delver/types"
)

type metricTransformer struct{}

func NewMetricTransformer() Transformer {
	return &metricTransformer{}
}

func (s *metricTransformer) Perform(msg json.RawMessage) (*types.Indexable, error) {
	return nil, nil
}

func (s *metricTransformer) Input() types.MessageType {
	return types.MetricType
}

func (s *metricTransformer) Streamable() bool {
	return false
}
