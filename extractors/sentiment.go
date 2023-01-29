package extractors

import (
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
	textContent := composite.Get(message.TitleExtractor).(string)
	language := composite.Get(message.LanguageExtractor).(features.Language)

	if language.Name != features.LangEnglish {
		return nil, nil
	}

	analysis := s.model.SentimentAnalysis(textContent, sentiment.English)
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
		message.TitleExtractor,
	}
}
