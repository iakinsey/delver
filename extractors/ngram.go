package extractors

import (
	"os"

	"github.com/iakinsey/delver/types/message"
)

type ngramExtractor struct{}

func NewNgramExtractor() Extractor {
	return &urlExtractor{}
}

func (s *ngramExtractor) Perform(f *os.File, meta message.FetcherResponse) (interface{}, error) {
	return nil, nil
}
