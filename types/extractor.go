package types

import (
	"fmt"

	"github.com/iakinsey/delver/types/features"
)

const (
	AdversarialExtractor = "adversarial"
	CompanyNameExtractor = "company_name"
	CountryExtractor     = "country"
	LanguageExtractor    = "language"
	NgramExtractor       = "ngram"
	SentimentExtractor   = "sentiment"
	TextExtractor        = "text"
	UrlExtractor         = "url"
)

var ExtractorNames = []string{
	AdversarialExtractor,
	CompanyNameExtractor,
	CountryExtractor,
	LanguageExtractor,
	NgramExtractor,
	SentimentExtractor,
	TextExtractor,
	UrlExtractor,
}

type CompositeAnalysis struct {
	Adversarial   *features.Adversarial
	Corporations  features.Corporations
	Countries     features.Countries
	Language      *features.Language
	Ngrams        *features.Ngrams
	TermFrequency *features.TermFrequency
	TextContent   features.TextContent
	Sentiment     *features.Sentiment
	URIs          features.URIs
}

func UpdateCompositeAnalysis(data interface{}, composite *CompositeAnalysis) error {
	switch d := data.(type) {
	case features.Adversarial:
		composite.Adversarial = &d
	case features.Corporations:
		composite.Corporations = d
	case features.Countries:
		composite.Countries = d
	case features.Language:
		composite.Language = &d
	case features.Ngrams:
		composite.Ngrams = &d
	case features.TermFrequency:
		composite.TermFrequency = &d
	case features.TextContent:
		composite.TextContent = d
	case features.Sentiment:
		composite.Sentiment = &d
	case features.URIs:
		composite.URIs = d
	default:
		return fmt.Errorf("attempt to cast unknown type in composite analysis")
	}

	return nil
}
