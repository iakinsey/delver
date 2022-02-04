package accumulator

import (
	"os"
	"testing"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/stretchr/testify/assert"
)

func TestDfsBasic(t *testing.T) {
	maxDepth := 1
	urlStorePath := util.NewTempPath("urlStore")
	visitedUrlsPath := util.NewTempPath("visitedUrls")

	defer os.Remove(visitedUrlsPath)
	defer os.RemoveAll(urlStorePath)

	accumulator := NewDfsBasicAccumulator(urlStorePath, visitedUrlsPath, maxDepth)
	msg1, _ := types.NewMessage(message.CompositeAnalysis{
		FetcherResponse: message.FetcherResponse{
			FetcherRequest: message.FetcherRequest{
				URI:   "http://example.com",
				Host:  "example.com",
				Depth: 0,
			},
		},
		URIs: features.URIs{
			"http://example.com/1",
			"http://example.com/2",
			"http://example.com/3",
			"http://example.com/4",
			"http://old.example.com/5",
			"http://non.com",
		},
	}, types.CompositeAnalysisType)

	out, err := accumulator.OnMessage(msg1)

	assert.NoError(t, err)
	assert.NotNil(t, out)
	assert.IsType(t, types.MultiMessage{}, out)

	mm, ok := out.(types.MultiMessage)

	assert.True(t, ok)
	assert.Len(t, mm.Values, 5)

	for _, ireq := range mm.Values {
		assert.IsType(t, message.FetcherRequest{}, ireq)

		req, ok := ireq.(message.FetcherRequest)

		assert.True(t, ok)
		assert.Contains(t, req.Host, "example.com")
	}

	out2, err := accumulator.OnMessage(msg1)

	assert.NoError(t, err)
	assert.NotNil(t, out2)
	assert.IsType(t, types.MultiMessage{}, out)

	mm2, ok := out2.(types.MultiMessage)

	assert.True(t, ok)
	assert.Len(t, mm2.Values, 0)
}

func TestDfsBasicMaxDepthExceeded(t *testing.T) {
	maxDepth := 1
	urlStorePath := util.NewTempPath("urlStore")
	visitedUrlsPath := util.NewTempPath("visitedUrls")

	defer os.Remove(visitedUrlsPath)
	defer os.RemoveAll(urlStorePath)

	accumulator := NewDfsBasicAccumulator(urlStorePath, visitedUrlsPath, maxDepth)
	msg1, _ := types.NewMessage(message.CompositeAnalysis{
		FetcherResponse: message.FetcherResponse{
			FetcherRequest: message.FetcherRequest{
				URI:   "http://example.com",
				Host:  "example.com",
				Depth: 1,
			},
		},
		URIs: features.URIs{
			"http://example.com/1",
			"http://example.com/2",
			"http://example.com/3",
			"http://example.com/4",
			"http://old.example.com/5",
			"http://non.com",
		},
	}, types.CompositeAnalysisType)

	out, err := accumulator.OnMessage(msg1)

	assert.NoError(t, err)
	assert.NotNil(t, out)
	assert.IsType(t, types.MultiMessage{}, out)

	mm, ok := out.(types.MultiMessage)

	assert.True(t, ok)
	assert.Len(t, mm.Values, 0)
}
