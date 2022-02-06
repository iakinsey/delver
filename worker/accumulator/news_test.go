package accumulator

import (
	"encoding/json"
	"testing"

	"github.com/iakinsey/delver/gateway/robots"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestNewsAccumulator(t *testing.T) {
	paths := testutil.SetupWorkerQueueFolders("NewsAccumulatorTest")

	defer testutil.TeardownWorkerQueueFolders(paths)

	queues := testutil.CreateQueueTriad(paths)
	newsQueue := queues.Outbox
	client := util.NewHTTPClient(util.HTTPClientParams{})
	memoryRobots := robots.NewMemoryRobots(client)
	accumulator := NewNewsAccumulator(newsQueue, memoryRobots)
	composite, _ := json.Marshal(message.CompositeAnalysis{
		FetcherResponse: message.FetcherResponse{
			FetcherRequest: message.FetcherRequest{
				URI: "http://test.com/example",
			},
		},
		URIs: features.URIs{
			"http://test.com",
			"http://example.com",
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
