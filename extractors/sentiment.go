package extractors

import (
	"log"
	"os"

	"github.com/cdipaolo/sentiment"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
)

type sentimentExtractor struct {
	model sentiment.Models
}

func NewSentimentExtractor() Extractor {
	model, err := sentiment.Restore()

	if err != nil {
		log.Fatalf("unable to restore sentiment model")
	}

	return &sentimentExtractor{
		model: model,
	}
}

func (s *sentimentExtractor) Perform(f *os.File, meta message.FetcherResponse, composite message.CompositeAnalysis) (interface{}, error) {
	if composite.Language.Name != features.LangEnglish {
		return nil, nil
	}

	analysis := s.model.SentimentAnalysis(string(composite.TextContent), sentiment.English)
	score := analysis.Score

	return features.Sentiment{
		BinaryNaiveBayesContent: &score,
	}, nil
}

func (s *sentimentExtractor) Name() string {
	return message.SentimentExtractor
}

func (s *sentimentExtractor) Requires() []string {
	return []string{
		message.LanguageExtractor,
	}
}
