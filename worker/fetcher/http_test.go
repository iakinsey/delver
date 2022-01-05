package fetcher

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util/testutil"
	"github.com/iakinsey/delver/worker"
)

func TestComposeFetcher(t *testing.T) {
	paths := testutil.SetupWorkerQueueFolders("HttpTest")

	defer testutil.TeardownWorkerQueueFolders(paths)

	queues := testutil.CreateQueueTriad(paths)

	fetcher := NewHttpFetcher(HttpFetcherArgs{
		UserAgent:   "test",
		StreamStore: queues.StreamStore,
	})

	message, _ := json.Marshal(message.FetcherRequest{
		RequestID: types.NewV4(),
		URI:       "http://google.com",
		Protocol:  types.ProtocolHTTP,
	})

	queues.Inbox.Put(types.Message{
		ID:          "0-0-0-TestName",
		MessageType: types.FetchResponse,
		Message:     json.RawMessage(message),
	}, 0)

	testutil.AssertFolderSize(t, paths.Inbox, 1)

	manager := worker.NewWorkerManager(fetcher, queues.Inbox, queues.Outbox)

	queues.Inbox.Start()
	go manager.Start()
	<-time.After(2 * time.Second)

	testutil.AssertFolderSize(t, paths.Inbox, 0)
	testutil.AssertFolderSize(t, paths.InboxDLQ, 0)
	testutil.AssertFolderSize(t, paths.Outbox, 1)
	testutil.AssertFolderSize(t, paths.OutboxDLQ, 0)
	testutil.AssertFolderSize(t, paths.StreamStore, 1)

	manager.Stop()
	queues.Inbox.Stop()
}
