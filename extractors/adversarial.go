package extractors

import (
	"os"

	"github.com/iakinsey/delver/types/message"
)

type adversarialExtractor struct{}

func NewAdversarialExtractor() Extractor {
	return &urlExtractor{}
}

func (s *adversarialExtractor) Perform(f *os.File, meta message.FetcherResponse) (interface{}, error) {
	return nil, nil
}
