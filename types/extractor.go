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

type CompositeAnalysis struct {
	Adversarial  *features.Adversarial
	Corporations features.Corporations
	Countries    features.Countries
	Language     *features.Language
	Ngrams       *features.Ngrams
	TextContent  features.TextContent
	Sentiment    *features.Sentiment
	URIs         features.URIs
}

func UpdateCompositeAnalysis(data interface{}, composite *CompositeAnalysis) (string, error) {
	var name string

	switch d := data.(type) {
	case features.Adversarial:
		name = AdversarialExtractor
		composite.Adversarial = &d
	case features.Corporations:
		name = CompanyNameExtractor
		composite.Corporations = d
	case features.Countries:
		name = CountryExtractor
		composite.Countries = d
	case features.Language:
		name = LanguageExtractor
		composite.Language = &d
	case features.Ngrams:
		name = NgramExtractor
		composite.Ngrams = &d
	case features.TextContent:
		name = TextExtractor
		composite.TextContent = d
	case features.Sentiment:
		name = SentimentExtractor
		composite.Sentiment = &d
	case features.URIs:
		name = UrlExtractor
		composite.URIs = d
	case error:
		return name, d
	case nil:
		return name, nil
	default:
		return name, fmt.Errorf("attempt to cast unknown type in composite analysis")
	}

	return name, nil
}
