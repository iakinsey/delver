package extractors

import (
	"context"
	"os"

	"github.com/pkg/errors"

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
		panic(err)
	}

	return &sentimentExtractor{
		model: model,
	}
}

func (s *sentimentExtractor) Perform(f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	return s.PerformContext(context.Background(), f, composite)
}
func (s *sentimentExtractor) PerformContext(ctx context.Context, f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var title string
	var language features.Language

	if err := composite.Load(features.TitleField, &title); err != nil {
		return nil, errors.Wrap(err, "sentiment extractor getting title")
	}

	if err := composite.Load(features.LanguageField, &language); err != nil {
		return nil, errors.Wrap(err, "sentiment extractor getting language")
	}

	if language.Name != features.LangEnglish {
		return nil, nil
	}

	if len(title) > 65536 {
		return nil, errors.New("title exceeds sentiment analysis limit")
	}
	analysis := s.model.SentimentAnalysis(title, sentiment.English)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	score := int32(analysis.Score)

	return features.Sentiment{
		BinaryNaiveBayesTitle: &score,
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
