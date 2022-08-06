package transformers

import (
	"encoding/json"

	"github.com/iakinsey/delver/types"
)

type pageTransformer struct{}

func NewPageTransformer() Transformer {
	return &pageTransformer{}
}

func (s *pageTransformer) Perform(msg json.RawMessage) ([]*types.Indexable, error) {

	return nil, nil
}

func (s *pageTransformer) Input() types.MessageType {
	return types.CompositeAnalysisType
}

func (s *pageTransformer) Streamable() bool {
	return true
}
