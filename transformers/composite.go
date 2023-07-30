package transformers

import (
	"encoding/json"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/pkg/errors"
)

type compositeTransformer struct{}

func NewCompositeTransformer() Transformer {
	return &compositeTransformer{}
}

func (s *compositeTransformer) Perform(msg json.RawMessage) ([]*types.Indexable, error) {
	var composite message.CompositeAnalysis
	var results []*types.Indexable

	if err := json.Unmarshal(msg, &composite); err != nil {
		return nil, errors.Wrap(err, "transformer failed to parse metrics")
	}

	results = append(results, &types.Indexable{
		ID:         string(composite.RequestID),
		Index:      s.Name(),
		DataType:   s.Name(),
		Data:       composite,
		Streamable: s.Streamable(),
	})

	return results, nil
}

func (s *compositeTransformer) Input() types.MessageType {
	return types.CompositeAnalysisType
}

func (s *compositeTransformer) Streamable() bool {
	return true
}

func (s *compositeTransformer) Name() string {
	return types.CompositeIndexable
}
