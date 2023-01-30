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
	var title string
	var language features.Language

	if err := composite.Load(features.TitleField, &title); err != nil {
		return nil, err
	}

	if err := composite.Load(features.LanguageField, &language); err != nil {
		return nil, err
	}

	if language.Name != features.LangEnglish {
		return nil, nil
	}

	analysis := s.model.SentimentAnalysis(title, sentiment.English)
	score := int32(analysis.Score)

	return features.Sentiment{
		BinaryNaiveBayesContent: &score,
	}, nil
}

func (s *sentimentExtractor) Name() string {
	return features.SentimentField
}

func (s *sentimentExtractor) Requires() []string {
	return []string{
		features.LanguageField,
		features.TitleField,
	}
}
