package extractors

import (
	"os"

	"github.com/iakinsey/delver/types/message"
)

type sentimentExtractor struct{}

func NewSentimentExtractor() Extractor {
	return &sentimentExtractor{}
}

func (s *sentimentExtractor) Perform(f *os.File, meta message.FetcherResponse) (interface{}, error) {
	return nil, nil
}
