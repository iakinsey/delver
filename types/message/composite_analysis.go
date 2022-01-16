package message

import (
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
	FetcherResponse

	Adversarial  *features.Adversarial `json:"adversarial,omitempty"`
	Corporations features.Corporations `json:"corporations,omitempty"`
	Countries    features.Countries    `json:"countries,omitempty"`
	Language     *features.Language    `json:"language,omitempty"`
	Ngrams       *features.Ngrams      `json:"ngrams,omitempty"`
	TextContent  features.TextContent  `json:"text_content,omitempty"`
	Sentiment    *features.Sentiment   `json:"sentiment,omitempty"`
	URIs         features.URIs         `json:"uris,omitempty"`
}
