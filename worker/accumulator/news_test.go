package accumulator

import (
	"encoding/json"
	"testing"

	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/resource/bloom"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestNewsAccumulator(t *testing.T) {
	paths := testutil.SetupWorkerQueueFolders("NewsAccumulatorTest")

	defer testutil.TeardownWorkerQueueFolders(paths)

	queues := testutil.CreateQueueTriad(paths)
	newsQueue := queues.Outbox
	accumulator := &newsAccumulator{
		maxDepth:  maxDepth,
		robots:    frontier.NewNullFilter(),
		newsQueue: newsQueue,
		seenUrls: bloom.NewBloomFilter(bloom.BloomFilterParams{
			MaxN: 1000,
			P:    0.01,
		}),
	}

	composite, _ := json.Marshal(message.CompositeAnalysis{
		FetcherResponse: message.FetcherResponse{
			FetcherRequest: message.FetcherRequest{
				URI: "http://test.com/example",
			},
		},
		Features: map[string]interface{}{
			message.UrlExtractor: features.URIs{
				"http://test.com/article/this-is-a-test-article-today",
				"http://example.com",
			},
		},
	})
	msg := types.Message{
		ID:          "0-0-0-TestName",
		MessageType: types.CompositeAnalysisType,
		Message:     json.RawMessage(composite),
	}

	result, err := accumulator.OnMessage(msg)

	assert.NoError(t, err)
	assert.IsType(t, types.MultiMessage{}, result)

	multiMessage := result.(types.MultiMessage)

	assert.Len(t, multiMessage.Values, 1)

	for _, value := range multiMessage.Values {
		assert.IsType(t, message.FetcherRequest{}, value)
	}
}
