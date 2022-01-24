package persist

import (
	"io"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
)

type ResourceFeatures struct {
	RequestID    types.UUID            `json:"request_id,omitempty"`
	URI          string                `json:"uri,omitempty"`
	Adversarial  *features.Adversarial `json:"adversarial,omitempty"`
	Corporations features.Corporations `json:"corporations,omitempty"`
	Countries    features.Countries    `json:"countries,omitempty"`
	Language     *features.Language    `json:"language,omitempty"`
	TextContent  features.TextContent  `json:"text_content,omitempty"`
	Sentiment    *features.Sentiment   `json:"sentiment,omitempty"`
	URIs         features.URIs         `json:"uris,omitempty"`
}

func NewResourceFeatures(composite message.CompositeAnalysis) ResourceFeatures {
	return ResourceFeatures{
		RequestID:    composite.RequestID,
		URI:          composite.URI,
		Adversarial:  composite.Adversarial,
		Corporations: composite.Corporations,
		Countries:    composite.Countries,
		Language:     composite.Language,
		TextContent:  composite.TextContent,
		Sentiment:    composite.Sentiment,
		URIs:         composite.URIs,
	}
}

func CompositeToResourceFeaturesParquet(composite message.CompositeAnalysis) (io.Reader, error) {
	resource := NewResourceFeatures(composite)

	return util.ToParquet(string(composite.RequestID), ResourceFeaturesParquetSchema, resource)
}

var ResourceFeaturesParquetSchema = `{
	"Tag": "name=resource, repetitiontype=REQUIRED",
	"Fields": [
		{"Tag": "name=request_id, inname=RequestID, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=uri, inname=URI, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
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
		{"Tag": "name=text_content, inname=TextContent, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{
			"Tag": "name=sentiment, inname=Sentiment, repetitiontype=OPTIONAL",
			"Fields": [
				{"Tag": "name=binary_naive_bayes_content, inname=BinaryNaiveBayesContent, type=INT32, repetitiontype=OPTIONAL"}
			]
		},
		{
			"Tag": "name=uris, inname=URIs, type=LIST, repetitiontype=REQUIRED",
			"Fields": [{"Tag": "name=element, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"}]
		}
	]
}`
