package extractors

import (
	"os"

	"github.com/iakinsey/delver/types/message"
)

type textExtractor struct{}

func NewTextExtractor() Extractor {
	return &textExtractor{}
}

func (s *textExtractor) Perform(f *os.File, meta message.FetcherResponse) (interface{}, error) {
	return nil, nil
}
