package extractors

import (
	"testing"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util/testutil"
	"github.com/stretchr/testify/assert"
)

const testCompanyNames = "company_names"

var expectedCompanyNames = features.Corporations{
	"AMEX:BATL",
	"NASDAQ:NXPI",
	"NYSE:FEI",
	"NYSE:MMS",
}

func TestCompanyNameExtractors(t *testing.T) {
	extractor := NewCompanyNameExtractor()
	textContent := features.TextContent(testutil.TestData(testCompanyNames))
	meta := message.FetcherResponse{}
	composite := types.CompositeAnalysis{
		TextContent: textContent,
	}

	corp, err := extractor.Perform(nil, meta, composite)

	assert.NoError(t, err)
	assert.NotNil(t, corp)
	assert.IsType(t, features.Corporations{}, corp)
	assert.ElementsMatch(t, expectedCompanyNames, corp)
}
