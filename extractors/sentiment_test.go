package extractors

import (
	"testing"

	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/assert"
)

var sentimentScenarios = map[string]uint8{
	"I am angry":          0,
	"I am happy":          1,
	"I am sad":            0,
	"We are angry":        0,
	"We are feeling good": 1,
}

func TestSentimentExtractor(t *testing.T) {
	extractor := NewSentimentExtractor()

	for text, expectedScore := range sentimentScenarios {
		textContent := features.TextContent(text)

		composite := message.CompositeAnalysis{
			FetcherResponse: message.FetcherResponse{},
			TextContent:     textContent,
			Language: &features.Language{
				Name: features.LangEnglish,
			},
		}

		sentiment, err := extractor.Perform(nil, composite)
		actualScore := (sentiment.(features.Sentiment)).BinaryNaiveBayesContent

		assert.NoError(t, err)
		assert.NotNil(t, sentiment)
		assert.IsType(t, features.Sentiment{}, sentiment)
		assert.Equal(t, &expectedScore, actualScore)
	}
}
