package extractors

import (
	"testing"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util/testutil"
	"github.com/stretchr/testify/assert"
)

const testCountryNames = "country_names"

var expectedCountries = features.Countries{"DEU", "KEN", "MCO", "USA"}

func TestCountryExtractor(t *testing.T) {
	extractor := NewCountryExtractor()
	textContent := features.TextContent(testutil.TestData(testCountryNames))
	meta := message.FetcherResponse{}
	composite := types.CompositeAnalysis{
		TextContent: textContent,
	}

	countries, err := extractor.Perform(nil, meta, composite)

	assert.NoError(t, err)
	assert.NotNil(t, countries)
	assert.IsType(t, features.Countries{}, countries)
	assert.ElementsMatch(t, expectedCountries, countries)
}
