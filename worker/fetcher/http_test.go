package fetcher

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestFetcher(t *testing.T) {
	paths := testutil.SetupWorkerQueueFolders("HttpTest")
	queues := testutil.CreateQueueTriad(paths)
	fetcher := &httpFetcher{
		MaxRetries:  1,
		ObjectStore: queues.ObjectStore,
		Client: &util.MockDelverHTTPClient{
			Error: nil,
			Response: &http.Response{
				Body:       io.NopCloser(strings.NewReader("test")),
				StatusCode: 200,
			},
		},
	}

	message, _ := json.Marshal(message.FetcherRequest{
		RequestID: types.NewV4(),
		URI:       "http://google.com",
		Protocol:  types.ProtocolHTTP,
	})

	msg := types.Message{
		ID:          "0-0-0-TestName",
		MessageType: types.FetcherRequestType,
		Message:     json.RawMessage(message),
	}

	res, err := fetcher.OnMessage(msg)

	assert.NoError(t, err)
	assert.NotNil(t, res)
	testutil.AssertFolderSize(t, paths.ObjectStore, 1)
}
