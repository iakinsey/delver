package extractors

import (
	"testing"

	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util/testutil"
	"github.com/stretchr/testify/assert"
)

const exampleHtmlFile = "example_html_file.html"

func TestUrlExtractor(t *testing.T) {
	extractor := NewUrlExtractor()
	htmlFile := testutil.TestDataFile(exampleHtmlFile)
	urls, err := extractor.Perform(htmlFile, message.CompositeAnalysis{})

	assert.NoError(t, err)
	assert.NotNil(t, urls)
	assert.IsType(t, features.URIs{}, urls)
	assert.Len(t, urls, 153)
}
