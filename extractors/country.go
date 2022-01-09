package extractors

import (
	"os"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
)

type countryExtractor struct{}

func NewCountryExtractor() Extractor {
	return &countryExtractor{}
}

func (s *countryExtractor) Perform(f *os.File, meta message.FetcherResponse, composite types.CompositeAnalysis) (interface{}, error) {
	return nil, nil
}

func (s *countryExtractor) Name() string {
	return types.CountryExtractor
}

func (s *countryExtractor) Requires() []string {
	return nil
}
