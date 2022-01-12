package extractors

import (
	"os"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
)

type sentimentExtractor struct{}

func NewSentimentExtractor() Extractor {
	return &sentimentExtractor{}
}

func (s *sentimentExtractor) Perform(f *os.File, meta message.FetcherResponse, composite types.CompositeAnalysis) (interface{}, error) {
	return nil, nil
}

func (s *sentimentExtractor) Name() string {
	return types.SentimentExtractor
}

func (s *sentimentExtractor) Requires() []string {
	return []string{
		types.TextExtractor,
	}
}
