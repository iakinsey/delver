package extractors

import (
	"os"

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

func (s *adversarialExtractor) Perform(f *os.File, meta message.FetcherResponse) (interface{}, error) {
	return nil, nil
}
