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

	Adversarial  *features.Adversarial `json:"adversarial"`
	Corporations features.Corporations `json:"corporations"`
	Countries    features.Countries    `json:"countries"`
	Language     *features.Language    `json:"language"`
	Ngrams       *features.Ngrams      `json:"ngrams"`
	TextContent  features.TextContent  `json:"text_content"`
	Sentiment    *features.Sentiment   `json:"sentiment"`
	URIs         features.URIs         `json:"uris"`
}
