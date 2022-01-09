package extractors

import (
	"os"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
)

type blacklistedExtractor struct{}

func NewBlacklistedExtractor() Extractor {
	return &blacklistedExtractor{}
}

func (s *blacklistedExtractor) Perform(f *os.File, meta message.FetcherResponse, composite types.CompositeAnalysis) (interface{}, error) {
	return nil, nil
}

func (s *blacklistedExtractor) Name() string {
	return types.BlacklistedExtractor
}

func (s *blacklistedExtractor) Requires() []string {
	return nil
}
