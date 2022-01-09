package extractors

import (
	"os"

	"github.com/iakinsey/delver/types/message"
)

type blacklistedExtractor struct{}

func NewBlacklistedExtractor() Extractor {
	return &urlExtractor{}
}

func (s *blacklistedExtractor) Perform(f *os.File, meta message.FetcherResponse) (interface{}, error) {
	return nil, nil
}
