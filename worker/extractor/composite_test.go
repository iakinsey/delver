package extractor

import (
	"encoding/json"
	"log"
	"testing"
	"time"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util/testutil"
	"github.com/iakinsey/delver/worker"
)

const exampleHtmlFile = "example_html_file.html"

func TestCompositeExtractorUrlOnly(t *testing.T) {
	paths := testutil.SetupWorkerQueueFolders("CompositeTest")

	defer testutil.TeardownWorkerQueueFolders(paths)

	queues := testutil.CreateQueueTriad(paths)
	htmlFile := testutil.TestDataFile(exampleHtmlFile)
	storeKey := types.NewV4()
	md5sum, err := queues.StreamStore.Put(storeKey, htmlFile)

	if err != nil {
		log.Fatalf(err.Error())
	}

	extractor := NewCompositeExtractorWorker(CompositeArgs{
		Extractors:  []string{types.UrlExtractor},
		StreamStore: queues.StreamStore,
	})

	message, _ := json.Marshal(message.FetcherResponse{
		StoreKey:      storeKey,
		ContentMD5:    md5sum,
		ElapsedTimeMs: 100,
		HTTPCode:      200,
		Success:       true,
		Timestamp:     time.Now().Unix(),
	})

	queues.Inbox.Put(types.Message{
		ID:          "0-0-0-TestName",
		MessageType: types.FetchResponse,
		Message:     json.RawMessage(message),
	}, 0)

	testutil.AssertFolderSize(t, paths.Inbox, 1)

	manager := worker.NewWorkerManager(extractor, queues.Inbox, queues.Outbox)

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
