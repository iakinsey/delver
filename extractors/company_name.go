package extractors

import (
	"os"

	"github.com/iakinsey/delver/types/message"
)

type companyNameExtractor struct{}

func NewCompanyNameExtractor() Extractor {
	return &companyNameExtractor{}
}

func (s *companyNameExtractor) Perform(f *os.File, meta message.FetcherResponse) (interface{}, error) {
	return nil, nil
}
