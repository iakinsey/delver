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

	for title, expectedScore := range sentimentScenarios {
		composite := message.CompositeAnalysis{
			Features: map[string]interface{}{
				features.TitleField: title,
				features.LanguageField: features.Language{
					Name: features.LangEnglish,
				},
			},
		}

		sentiment, err := extractor.Perform(nil, composite)
		actualScore := uint8(*(sentiment.(features.Sentiment)).BinaryNaiveBayesContent)

		assert.NoError(t, err)
		assert.NotNil(t, sentiment)
		assert.IsType(t, features.Sentiment{}, sentiment)
		assert.Equal(t, &expectedScore, &actualScore)
	}
}
