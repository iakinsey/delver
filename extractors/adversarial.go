package extractors

import (
	"os"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
)

const subdomainThreshold = 25

type adversarialExtractor struct {
	subdomainThreshold int32
}

func NewAdversarialExtractor() Extractor {
	return &adversarialExtractor{
		subdomainThreshold: subdomainThreshold,
	}
}

func (s *adversarialExtractor) Perform(f *os.File, meta message.FetcherResponse, composite types.CompositeAnalysis) (interface{}, error) {
	return nil, nil
}

func (s *adversarialExtractor) Name() string {
	return types.AdversarialExtractor
}

func (s *adversarialExtractor) Requires() []string {
	return []string{
		types.UrlExtractor,
	}
}

func (s *adversarialExtractor) detectEnumeration(urls []string) bool {
	return false
}
