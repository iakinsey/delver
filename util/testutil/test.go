package testutil

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/resource/objectstore"
	"github.com/iakinsey/delver/util"
	"github.com/stretchr/testify/assert"
)

type QueuePaths struct {
	Inbox       string
	InboxDLQ    string
	Outbox      string
	OutboxDLQ   string
	ObjectStore string
}

type TestQueues struct {
	Inbox       queue.Queue
	Outbox      queue.Queue
	ObjectStore objectstore.ObjectStore
}

func AssertFolderSize(t *testing.T, path string, length int) {
	files, err := ioutil.ReadDir(path)

	assert.NoError(t, err)
	assert.Equal(t, len(files), length)
}

func SetupWorkerQueueFolders(prefix string) QueuePaths {
	// Delete previous temp files that may exist
	files, err := ioutil.ReadDir(os.TempDir())

	if err != nil {
		log.Panicf(err.Error())
	}

	for _, file := range files {
		if !strings.HasPrefix(file.Name(), prefix) {
			continue
		}
		path := filepath.Join(os.TempDir(), file.Name())
		err := os.RemoveAll(path)

		if err != nil {
			log.Panicf(err.Error())
		}
	}

	// Set up new temp files
	paths := QueuePaths{}
	values := reflect.Indirect(reflect.ValueOf(&paths))

	for i := 0; i < values.NumField(); i++ {
		name := values.Field(i).Type().Name()
		path := util.MakeTempFolder(prefix + name)

		values.Field(i).SetString(path)
	}

	return paths
}

func TeardownWorkerQueueFolders(paths QueuePaths) {
	values := reflect.ValueOf(paths)

	for i := 0; i < values.NumField(); i++ {
		path := values.Field(i).String()
		os.RemoveAll(path)
	}
}

func CreateQueueTriad(paths QueuePaths) (queues TestQueues) {
	queues.Inbox = CreateTestQueue(paths.Inbox, paths.InboxDLQ)
	queues.Outbox = CreateTestQueue(paths.Outbox, paths.OutboxDLQ)
	params := objectstore.FilesystemObjectStoreParams{Path: paths.ObjectStore}
	objectStore := objectstore.NewFilesystemObjectStore(params)
	queues.ObjectStore = objectStore

	return queues
}

func CreateFileQueue(name string) (queue.Queue, string, string) {
	inbox := util.MakeTempFolder(name)
	dlq := util.MakeTempFolder(name)

	return CreateTestQueue(inbox, dlq), inbox, dlq
}

func TestDataFile(name string) *os.File {
	testDataPath := config.DataFilePath("data", "test")
	p := filepath.Join(testDataPath, name)
	file, err := os.Open(p)

	if err != nil {
		log.Fatalf((err.Error()))
	}

	return file
}

func TestData(name string) []byte {
	f := TestDataFile(name)
	data, err := ioutil.ReadAll(f)

	if err != nil {
		log.Fatalf(err.Error())
	}

	return data
}

func CreateTestQueue(path string, dlq string) queue.Queue {
	return queue.NewFileQueue(queue.FileQueueParams{
		Name:           "TestInboxQueue",
		Path:           path,
		DlqPath:        dlq,
		MaxPollDelayMs: 100,
		MaxSize:        100,
		Reset:          false,
		Resilient:      false,
	})
}
