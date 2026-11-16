package extractors

import (
	"github.com/iakinsey/delver/config"
	"testing"

	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util/testutil"
	"github.com/stretchr/testify/assert"
)

const testCountryNames = "country_names"

var expectedCountries = features.Countries{"DEU", "KEN", "MCO", "USA"}

func TestCountryExtractor(t *testing.T) {
	conf := config.LoadConfig()
	conf.CountriesPath = "testdata/countries.json"
	extractor := NewCountryExtractorWithConfig(conf)
	textContent := testutil.TestData(testCountryNames)
	composite := message.CompositeAnalysis{
		Features: map[string]interface{}{
			features.TextField: string(textContent),
		},
	}

	countries, err := extractor.Perform(nil, composite)

	assert.NoError(t, err)
	assert.NotNil(t, countries)
	assert.IsType(t, features.Countries{}, countries)
	assert.ElementsMatch(t, expectedCountries, countries)
}
