package extractors

import (
	"fmt"
	"os"

	log "github.com/sirupsen/logrus"

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

func (s *sentimentExtractor) Perform(f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	if composite.Language.Name != features.LangEnglish {
		return nil, nil
	}

	analysis := s.model.SentimentAnalysis(string(composite.TextContent), sentiment.English)
	score := int32(analysis.Score)

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

func (s *sentimentExtractor) SetResult(result interface{}, composite *message.CompositeAnalysis) error {
	switch d := result.(type) {
	case features.Sentiment:
		composite.Sentiment = &d
		return nil
	default:
		return fmt.Errorf("SentimentExtractor: attempt to cast unknown type")
	}
}
