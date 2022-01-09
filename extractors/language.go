package extractors

import (
	"os"

	"github.com/iakinsey/delver/types/message"
)

type languageExtractor struct{}

func NewLanguageExtractor() Extractor {
	return &urlExtractor{}
}

func (s *languageExtractor) Perform(f *os.File, meta message.FetcherResponse) (interface{}, error) {
	return nil, nil
}
