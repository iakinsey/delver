package extractors

import (
	"testing"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util/testutil"
	"github.com/stretchr/testify/assert"
)

const lipsumHtml = "lipsum.html"

func TestTextExtractor(t *testing.T) {
	extractor := NewTextExtractor()
	f := testutil.TestDataFile(lipsumHtml)
	meta := message.FetcherResponse{}
	composite := types.CompositeAnalysis{}

	text, err := extractor.Perform(f, meta, composite)
	assert.NoError(t, err)
	assert.NotNil(t, text)
	assert.IsType(t, features.TextContent{}, text)
	assert.Len(t, text, 3596)
}
