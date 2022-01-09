package extractors

import (
	"os"

	"github.com/iakinsey/delver/types/message"
)

type countryExtractor struct{}

func NewCountryExtractor() Extractor {
	return &urlExtractor{}
}

func (s *countryExtractor) Perform(f *os.File, meta message.FetcherResponse) (interface{}, error) {
	return nil, nil
}
