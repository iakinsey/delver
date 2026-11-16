package extractors

import (
	"testing"

	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util/testutil"
	"github.com/stretchr/testify/assert"
)

const lipsumHtml = "lipsum.html"

func TestTextExtractor(t *testing.T) {
	extractor := NewTextExtractor()
	f := testutil.TestDataFile(lipsumHtml)
	text, err := extractor.Perform(f, message.CompositeAnalysis{})

	assert.NoError(t, err)
	assert.NotNil(t, text)
	assert.IsType(t, "", text)
	assert.Len(t, text, 3596)
}
