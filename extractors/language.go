package extractors

import (
	"os"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
)

type languageExtractor struct{}

func NewLanguageExtractor() Extractor {
	return &languageExtractor{}
}

func (s *languageExtractor) Perform(f *os.File, meta message.FetcherResponse) (interface{}, error) {
	return nil, nil
}

func (s *languageExtractor) Name() string {
	return types.LanguageExtractor
}

func (s *languageExtractor) Requires() []string {
	return nil
}
