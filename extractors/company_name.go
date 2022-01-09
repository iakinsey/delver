package extractors

import (
	"os"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
)

type companyNameExtractor struct{}

func NewCompanyNameExtractor() Extractor {
	return &companyNameExtractor{}
}

func (s *companyNameExtractor) Perform(f *os.File, meta message.FetcherResponse, composite types.CompositeAnalysis) (interface{}, error) {
	return nil, nil
}

func (s *companyNameExtractor) Name() string {
	return types.CompanyNameExtractor
}

func (s *companyNameExtractor) Requires() []string {
	return nil
}
