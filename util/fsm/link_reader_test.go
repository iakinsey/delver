package fsm

import (
	"testing"

	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/util/testutil"
	"github.com/stretchr/testify/assert"
)

const urlListPath = "url_list_link_reader"

func TestLinkReader(t *testing.T) {
	linkReader := NewFSM(NewLinkReaderFSM())
	actualUrls, err := linkReader.Perform(testutil.TestDataFile(urlListPath))

	assert.NoError(t, err)

	expectedUrls, err := util.ReadLines(testutil.TestDataFile(urlListPath))

	assert.NoError(t, err)
	assert.Equal(t, len(expectedUrls), len(actualUrls))

	for index, expected := range expectedUrls {
		assert.Contains(t, actualUrls[index], expected)
	}
}
