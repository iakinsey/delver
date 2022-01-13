package extractors

import (
	"os"

	"github.com/abadojack/whatlanggo"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
)

type languageExtractor struct{}

func NewLanguageExtractor() Extractor {
	return &languageExtractor{}
}

func (s *languageExtractor) Perform(f *os.File, meta message.FetcherResponse, composite types.CompositeAnalysis) (interface{}, error) {
	info := whatlanggo.Detect(string(composite.TextContent))
	return features.Language{
		Name:       info.Lang.Iso6391(),
		Confidence: info.Confidence,
	}, nil
}

func (s *languageExtractor) Name() string {
	return types.LanguageExtractor
}

func (s *languageExtractor) Requires() []string {
	return []string{
		types.TextExtractor,
	}
}
