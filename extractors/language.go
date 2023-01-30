package extractors

import (
	"os"

	"github.com/abadojack/whatlanggo"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
)

type languageExtractor struct{}

func NewLanguageExtractor() Extractor {
	return &languageExtractor{}
}

func (s *languageExtractor) Perform(f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	var textContent string

	if err := composite.Load(features.TextField, &textContent); err != nil {
		return nil, err
	}

	info := whatlanggo.Detect(textContent)

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
