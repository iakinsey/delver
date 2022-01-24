package accumulator

import (
	"encoding/json"
	"testing"

	"github.com/iakinsey/delver/gateway/logger"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/assert"
)

const enableResourceTest = false

func TestResourceAccumulator(t *testing.T) {
	if !enableResourceTest {
		return
	}

	loggers := []logger.Logger{
		logger.NewElasticsearchLogger([]string{"http://localhost:9200"}),
		logger.NewHDFSLogger("localhost:9000"),
	}
	accumulator := NewResourceAccumulator(loggers)
	composite, _ := json.Marshal(message.CompositeAnalysis{
		FetcherResponse: message.FetcherResponse{
			FetcherRequest: message.FetcherRequest{
				RequestID: "test-request-id",
			},
		},
	})
	msg := types.Message{
		ID:          "0-0-0-TestName",
		MessageType: types.FetcherResponseType,
		Message:     json.RawMessage(composite),
	}

	res, err := accumulator.OnMessage(msg)

	assert.Nil(t, res)
	assert.NoError(t, err)
}
