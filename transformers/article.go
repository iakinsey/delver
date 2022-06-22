package transformers

import (
	"encoding/json"

	"github.com/iakinsey/delver/types"
)

type articleTransformer struct{}

func NewArticleTransformer() Transformer {
	return &articleTransformer{}
}

func (s *articleTransformer) Perform(msg json.RawMessage) (*types.Indexable, error) {
	return nil, nil
}

func (s *articleTransformer) Input() types.MessageType {
	return types.CompositeAnalysisType
}

func (s *articleTransformer) Streamable() bool {
	return true
}
