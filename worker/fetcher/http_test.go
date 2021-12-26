package fetcher

import (
	"encoding/json"
	"log"
	"os"
	"testing"

	"github.com/iakinsey/delver/gateway/streamstore"
	"github.com/iakinsey/delver/model"
	"github.com/iakinsey/delver/model/message"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/worker"
)

func TestComposeFetcher(t *testing.T) {
	inboxPath := util.MakeTempFile("TestInboxQueue")
	inboxDlqPath := util.MakeTempFile("TestInboxDlq")
	outboxPath := util.MakeTempFile("TestOutboxQueue")
	outboxDlqPath := util.MakeTempFile("TestOutboxDlq")
	streamStorePath := util.MakeTempFile("TestStreamStore")
	maxCoroutines := 1

	defer os.Remove(inboxPath)
	defer os.Remove(inboxDlqPath)
	defer os.Remove(outboxPath)
	defer os.Remove(outboxDlqPath)
	defer os.Remove(streamStorePath)

	inbox, err := queue.NewFileQueue("TestInboxQueue", inboxPath, inboxDlqPath, 100, 100, false)

	if err != nil {
		log.Fatalf(err.Error())
	}

	outbox, err := queue.NewFileQueue("TestOutboxQueue", inboxPath, inboxDlqPath, 100, 100, false)

	if err != nil {
		log.Fatalf(err.Error())
	}

	streamStore, err := streamstore.NewFilesystemStreamStore(streamStorePath)

	if err != nil {
		log.Fatalf(err.Error())
	}

	fetcher := NewHttpFetcher(HttpFetcherArgs{
		UserAgent:   "test",
		StreamStore: streamStore,
	})

	message, _ := json.Marshal(message.FetcherRequest{
		RequestID: types.NewV4(),
		URI:       "http://google.com",
		Protocol:  types.ProtocolHTTP,
	})

	inbox.Put(model.Message{
		ID:          "0-0-0-TestName",
		MessageType: types.FetchResponse,
		Message:     json.RawMessage(message),
	}, 0)

	manager := worker.NewWorkerManager(fetcher, inbox, outbox, maxCoroutines)

	manager.Start()

	manager.Stop()
}
