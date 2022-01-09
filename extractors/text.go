package extractors

import (
	"os"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
)

type textExtractor struct{}

func NewTextExtractor() Extractor {
	return &textExtractor{}
}

func (s *textExtractor) Perform(f *os.File, meta message.FetcherResponse, composite types.CompositeAnalysis) (interface{}, error) {
	return nil, nil
}

func (s *textExtractor) Name() string {
	return types.TextExtractor
}

func (s *textExtractor) Requires() []string {
	return nil
}
