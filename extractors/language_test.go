package extractors

import (
	"testing"

	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/assert"
)

var langScenarios = map[string]string{
	"The quick brown fox jumps over the lazy dog.":              "en",
	"你来自哪里？":                                                    "zh",
	"¿Cómo se dice ‘concert’ en español?":                       "es",
	"لِنَذْهَبْ إِلَى السِّيْنَمَا":                             "ar",
	"Qu’est-ce que vous aimez faire pendant votre temps libre?": "fr",
	"Можно заплатить кредитной карточкой?":                      "ru",
}

func TestLanguageExtractor(t *testing.T) {
	extractor := NewLanguageExtractor()

	for text, expectedLang := range langScenarios {
		textContent := features.TextContent(text)

		composite := message.CompositeAnalysis{
			TextContent: textContent,
		}

		lang, err := extractor.Perform(nil, composite)
		actualLang := lang.(features.Language).Name

		assert.NoError(t, err)
		assert.NotNil(t, lang)
		assert.IsType(t, features.Language{}, lang)
		assert.Equal(t, expectedLang, actualLang)
	}
}
