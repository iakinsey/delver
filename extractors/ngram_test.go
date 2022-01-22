package extractors

/*
import (
	"testing"

	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/assert"
)

var scenarios = map[string][][]string{
	"The quick brown fox jumps over the lazy dog.": {
		{"the", "quick", "brown"},
		{"quick", "brown", "fox"},
		{"brown", "fox", "jumps"},
		{"fox", "jumps", "over"},
		{"jumps", "over", "the"},
		{"over", "the", "lazy"},
		{"the", "lazy", "dog"},
	},
	"Waltz, bad nymph, for quick jigs vex": {
		{"for", "quick", "jigs"},
		{"quick", "jigs", "vex"},
	},
	"Pack my box with five dozen liquor jugs": {
		{"pack", "my", "box"},
		{"my", "box", "with"},
		{"box", "with", "five"},
		{"with", "five", "dozen"},
		{"five", "dozen", "liquor"},
		{"dozen", "liquor", "jugs"},
	},
	"The, five; boxing' wizards[] jump quickly": {},
}

func TestNgramExtractor(t *testing.T) {
	extractor := NewNgramExtractor()

	for basicText, expectedNgrams := range scenarios {
		composite := message.CompositeAnalysis{
			FetcherResponse: message.FetcherResponse{},
			TextContent:     features.TextContent(basicText),
		}
		ngrams, err := extractor.Perform(nil, composite)

		assert.NoError(t, err)
		assert.NotNil(t, ngrams)
		assert.IsType(t, features.Ngrams{}, ngrams)
		assert.ElementsMatch(t, expectedNgrams, ngrams.(features.Ngrams)[defaultN])
	}
}
*/
