package extractors

import (
	"context"
	"os"

	"github.com/abadojack/whatlanggo"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/pkg/errors"
)

type languageExtractor struct{}

func NewLanguageExtractor() Extractor {
	return &languageExtractor{}
}

func (s *languageExtractor) Perform(f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	return s.PerformContext(context.Background(), f, composite)
}
func (s *languageExtractor) PerformContext(ctx context.Context, f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var textContent string

	if err := composite.Load(features.TextField, &textContent); err != nil {
		return nil, errors.Wrap(err, "language extractor")
	}

	if len(textContent) > 65536 {
		textContent = textContent[:65536]
	}
	info := whatlanggo.Detect(textContent)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return features.Language{
		Name:       info.Lang.Iso6391(),
		Confidence: info.Confidence,
	}, nil
}

func (s *languageExtractor) Name() string {
	return features.LanguageField
}

func (s *languageExtractor) Requires() []string {
	return []string{
		features.TextField,
	}
}
