package fsm

import (
	"testing"

	"github.com/iakinsey/delver/util/testutil"
	"github.com/stretchr/testify/assert"
)

const urlListName = "example_html_file.html"

func TestDocumentReader(t *testing.T) {
	docReader := NewFSM(NewDocumentReaderFSM())
	actualUrls, err := docReader.Perform(testutil.TestDataFile(urlListName))

	assert.NoError(t, err)
	assert.Equal(t, 164, len(actualUrls))
}
