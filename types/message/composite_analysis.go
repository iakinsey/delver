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

var ParquetSchema = `{
	"Tag": "name=resource, repetitiontype=REQUIRED",
	"Fields": [
		{"Tag": "name=request_id, inname=RequestID, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=uri, inname=URI, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=host, inname=Host, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=origin, inname=Origin, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=protocol, inname=Protocol, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=store_key, inname=StoreKey, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=content_md5, inname=ContentMD5, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=elapsed_time_ms, inname=ElapsedTimeMs, type=INT64, convertedtype=INT_64, repetitiontype=REQUIRED"},
		{"Tag": "name=error, inname=Error, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=http_code, inname=HTTPCode, type=INT32, convertedtype=UINT_16, repetitiontype=REQUIRED"},
		{"Tag": "name=success, inname=Success, type=BOOLEAN, repetitiontype=REQUIRED"},
		{"Tag": "name=timestamp, inname=Timestamp, type=INT64, convertedtype=INT_64, repetitiontype=REQUIRED"},
		{
			"Tag": "name=header, inname=Header, type=MAP, repetitiontype=REQUIRED",
			"Fields": [
				{"Tag": "name=key, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
				{
					"Tag": "name=value, type=LIST, repetitiontype=REQUIRED",
					"Fields": [
						{"Tag": "name=element, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"}
					]
				}
			]
		},
		{
			"Tag": "name=adversarial, inname=Adversarial, repetitiontype=OPTIONAL",
			"Fields": [
				{"Tag": "name=enumeration, inname=Enumeration, type=BOOLEAN, repetitiontype=OPTIONAL"},
				{"Tag": "name=enumeration, inname=SubdomainExplosion, type=BOOLEAN, repetitiontype=OPTIONAL"}
			]
		},
    	{
			"Tag": "name=corporations, inname=Corporations, type=LIST, repetitiontype=REQUIRED",
			"Fields": [{"Tag": "name=element, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"}]
		},
    	{
			"Tag": "name=countries, inname=Countries, type=LIST, repetitiontype=REQUIRED",
			"Fields": [{"Tag": "name=element, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"}]
		},
		{
			"Tag": "name=language, inname=Language, repetitiontype=OPTIONAL",
			"Fields": [
				{"Tag": "name=name, inname=Name, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
				{"Tag": "name=confidence, inname=Confidence, type=DOUBLE, repetitiontype=REQUIRED"}
			]
		},
		{
			"Tag": "name=ngrams, inname=Ngrams, type=MAP, repetitiontype=OPTIONAL",
			"Fields": [
				{"Tag": "name=key, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
				{
					"Tag": "name=value, type=LIST, repetitiontype=REQUIRED",
					"Fields": [
						{
							"Tag": "name=value, type=LIST, repetitiontype=REQUIRED",
							"Fields": [
								{"Tag": "name=element, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"}
							]
						}
					]
				}
			]
		},
		{"Tag": "name=text_content, inname=TextContent, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{
			"Tag": "name=sentiment, inname=Sentiment, repetitiontype=OPTIONAL",
			"Fields": [
				{"Tag": "name=binary_naive_bayes_summary, inname=BinaryNaiveBayesSummary, type=INT32, convertedtype=UINT_8, repetitiontype=OPTIONAL"},
				{"Tag": "name=binary_naive_bayes_content, inname=BinaryNaiveBayesContent, type=INT32, convertedtype=UINT_8, repetitiontype=OPTIONAL"},
				{"Tag": "name=binary_naive_bayes_title, inname=BinaryNaiveBayesTitle, type=INT32, convertedtype=UINT_8, repetitiontype=OPTIONAL"},
				{"Tag": "name=binary_naive_bayes_aggregate, inname=BinaryNaiveBayesAggregate, type=DOUBLE, repetitiontype=OPTIONAL"}
			]
		},
		{
			"Tag": "name=uris, inname=URIs, type=LIST, repetitiontype=REQUIRED",
			"Fields": [{"Tag": "name=element, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"}]
		}
	]
}`
