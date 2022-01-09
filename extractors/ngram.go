package extractors

import (
	"os"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
)

type ngramExtractor struct{}

func NewNgramExtractor() Extractor {
	return &ngramExtractor{}
}

func (s *ngramExtractor) Perform(f *os.File, meta message.FetcherResponse, composite types.CompositeAnalysis) (interface{}, error) {
	return nil, nil
}

func (s *ngramExtractor) Name() string {
	return types.NgramExtractor
}

func (s *ngramExtractor) Requires() []string {
	return nil
}
