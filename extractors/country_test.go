package extractors

import (
	"testing"

	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util/testutil"
	"github.com/stretchr/testify/assert"
)

const testCountryNames = "country_names"

var expectedCountries = features.Countries{"DEU", "KEN", "MCO", "USA"}

func TestCountryExtractor(t *testing.T) {
	extractor := NewCountryExtractor()
	textContent := testutil.TestData(testCountryNames)
	composite := message.CompositeAnalysis{
		Features: map[string]interface{}{
			message.TextExtractor: string(textContent),
		},
	}

	countries, err := extractor.Perform(nil, composite)

	assert.NoError(t, err)
	assert.NotNil(t, countries)
	assert.IsType(t, features.Countries{}, countries)
	assert.ElementsMatch(t, expectedCountries, countries)
}
