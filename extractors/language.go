package extractors

import (
	"fmt"
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
	info := whatlanggo.Detect(string(composite.TextContent))
	return features.Language{
		Name:       info.Lang.Iso6391(),
		Confidence: info.Confidence,
	}, nil
}

func (s *languageExtractor) Name() string {
	return message.LanguageExtractor
}

func (s *languageExtractor) Requires() []string {
	return []string{
		message.TextExtractor,
	}
}

func (s *languageExtractor) SetResult(result interface{}, composite *message.CompositeAnalysis) error {
	switch d := result.(type) {
	case features.Language:
		composite.Language = &d
		return nil
	default:
		return fmt.Errorf("LanguageExtractor: attempt to cast unknown type")
	}
}
