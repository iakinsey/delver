package fetcher

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iakinsey/delver/gateway/streamstore"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/worker"
	"github.com/stretchr/testify/assert"
)

const folderPrefix = "HttpTest"

func assertFolderLength(t *testing.T, path string, length int) {
	files, err := ioutil.ReadDir(path)

	assert.NoError(t, err)
	assert.Equal(t, len(files), length)
}

func setup() {
	files, err := ioutil.ReadDir(os.TempDir())

	if err != nil {
		log.Panicf(err.Error())
	}

	for _, file := range files {
		if !strings.HasPrefix(file.Name(), folderPrefix) {
			continue
		}
		path := filepath.Join(os.TempDir(), file.Name())
		err := os.RemoveAll(path)

		if err != nil {
			log.Panicf(err.Error())
		}
	}
}

func TestComposeFetcher(t *testing.T) {
	setup()
	inboxPath := util.MakeTempFile(folderPrefix + "InboxQueue")
	inboxDlqPath := util.MakeTempFile(folderPrefix + "InboxDlq")
	outboxPath := util.MakeTempFile(folderPrefix + "OutboxQueue")
	outboxDlqPath := util.MakeTempFile(folderPrefix + "OutboxDlq")
	streamStorePath := util.MakeTempFile(folderPrefix + "StreamStore")

	defer os.Remove(inboxPath)
	defer os.Remove(inboxDlqPath)
	defer os.Remove(outboxPath)
	defer os.Remove(outboxDlqPath)
	defer os.Remove(streamStorePath)

	inbox, err := queue.NewFileQueue("TestInboxQueue", inboxPath, inboxDlqPath, 100, 100, false)

	if err != nil {
		log.Fatalf(err.Error())
	}

	outbox, err := queue.NewFileQueue("TestOutboxQueue", outboxPath, outboxDlqPath, 100, 100, false)

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

	inbox.Put(types.Message{
		ID:          "0-0-0-TestName",
		MessageType: types.FetchResponse,
		Message:     json.RawMessage(message),
	}, 0)

	assertFolderLength(t, inboxPath, 1)

	manager := worker.NewWorkerManager(fetcher, inbox, outbox)

	inbox.Start()
	go manager.Start()
	<-time.After(2 * time.Second)

	assertFolderLength(t, inboxPath, 0)
	assertFolderLength(t, inboxDlqPath, 0)
	assertFolderLength(t, outboxPath, 1)
	assertFolderLength(t, outboxDlqPath, 0)
	assertFolderLength(t, streamStorePath, 1)

	manager.Stop()
	inbox.Stop()
}
