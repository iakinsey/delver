package queue

import (
	"encoding/json"
	"github.com/iakinsey/delver/types"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCrawlRecoveryPreservesOutputs(t *testing.T) {
	q, err := NewCrawlQueue(CrawlQueueParams{Path: filepath.Join(t.TempDir(), "crawl"), CrawlID: "recovery", MaxAttempts: 1})
	require.NoError(t, err)
	s := q.(*crawlQueue)
	defer s.Close()
	msg := crawlRequest(t, "http://example.test/")
	require.NoError(t, s.Put(msg, 0))
	require.NoError(t, s.Start())
	msg = receiveCrawl(t, s)
	outputs := []types.Message{{ID: "stable-output", Message: json.RawMessage(`{}`)}}
	require.NoError(t, s.SaveOutputs(msg, outputs))
	require.NoError(t, s.EndTransaction(msg, false))
	failed, err := s.Failed(0, 50)
	require.NoError(t, err)
	require.Len(t, failed, 1)
	require.NoError(t, s.Recover(failed[0].ID, false))
	again := receiveCrawl(t, s)
	require.Equal(t, msg.ID, again.ID)
	staged, found, err := s.LoadOutputs(again)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, outputs, staged)
	require.NoError(t, s.EndTransaction(again, true))
	require.NoError(t, s.Recover(failed[0].ID, true))
	recrawl := receiveCrawl(t, s)
	require.NotEqual(t, msg.ID, recrawl.ID)
}

func TestFileRecoveryKeepsStagedBatchAndRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	s := NewFileQueue(FileQueueParams{Name: "recovery", Path: filepath.Join(root, "queue"), DlqPath: filepath.Join(root, "failed"), MaxAttempts: 1, MaxPollDelayMs: 1, Reset: true, Resilient: true}).(*fileQueue)
	require.NoError(t, s.Start())
	defer s.Stop()
	msg := types.Message{ID: "original", Message: json.RawMessage(`{}`)}
	require.NoError(t, s.Put(msg, 0))
	select {
	case msg = <-s.GetChannel():
	case <-time.After(time.Second):
		t.Fatal("no delivery")
	}
	require.NoError(t, s.SaveOutputs(msg, []types.Message{{ID: "stable", Message: json.RawMessage(`{}`)}}))
	require.NoError(t, s.EndTransaction(msg, false))
	records, err := s.Failed(0, 50)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, 1, records[0].Attempts)
	require.Error(t, s.Recover("../outside", false))
	require.NoError(t, s.Recover(records[0].ID, false))
	select {
	case msg = <-s.GetChannel():
	case <-time.After(time.Second):
		t.Fatal("no replay")
	}
	require.Equal(t, 0, msg.Attempts)
	outputs, found, err := s.LoadOutputs(msg)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "stable", outputs[0].ID)
	require.NoError(t, s.EndTransaction(msg, true))
}

func TestFileReplayRestoresEveryInterruptedPhase(t *testing.T) {
	for _, phase := range []string{"manifest", "outputs", "message", "tombstone", "source-removed"} {
		t.Run(phase, func(t *testing.T) {
			q := testFileQueue(t)
			id := "0-1-0-regression"
			source := filepath.Join(q.dlqPath, id)
			target := filepath.Join(q.path, id)
			msg := types.Message{ID: "original", Message: json.RawMessage(`{"store_key":"retained-body"}`), MessageType: types.FetcherResponseType}
			outputs := json.RawMessage(`[{"id":"stable-output","message_type":3,"message":{}}]`)
			manifest, err := json.Marshal(fileReplay{Message: msg, Outputs: outputs})
			require.NoError(t, err)
			data, _ := json.Marshal(msg)
			require.NoError(t, os.WriteFile(source, data, 0600))
			require.NoError(t, writeRecovery(source+".replay", manifest))
			if phase != "manifest" {
				require.NoError(t, writeRecovery(target+claimedSuffix+".outputs", outputs))
			}
			if phase == "message" || phase == "tombstone" || phase == "source-removed" {
				require.NoError(t, writeRecovery(target, data))
			}
			if phase == "tombstone" || phase == "source-removed" {
				require.NoError(t, writeRecovery(source+".replayed", []byte(`{}`)))
			}
			if phase == "source-removed" {
				require.NoError(t, os.Remove(source))
			}
			refs, err := q.ReferencedObjects()
			require.NoError(t, err)
			require.Contains(t, refs, types.UUID("retained-body"))
			require.NoError(t, q.Start())
			defer q.Stop()
			select {
			case delivered := <-q.GetChannel():
				require.Equal(t, "original", delivered.ID)
				staged, found, err := q.LoadOutputs(delivered)
				require.NoError(t, err)
				require.True(t, found)
				require.Equal(t, "stable-output", staged[0].ID)
				require.NoError(t, q.EndTransaction(delivered, true))
			case <-time.After(time.Second):
				t.Fatal("replay was not restored")
			}
			require.NoError(t, q.Recover(id, false))
		})
	}
}
func TestConcurrentFileRecoveryDoesNotDuplicateRecord(t *testing.T) {
	q := testFileQueue(t)
	id := "0-1-0-regression"
	data, _ := json.Marshal(types.Message{ID: "stable", Message: json.RawMessage(`{}`)})
	require.NoError(t, os.WriteFile(filepath.Join(q.dlqPath, id), data, 0600))
	var wg sync.WaitGroup
	errors := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errors <- q.Recover(id, false) }()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, q.Len())
}
func TestRecoveryStorageFailurePreservesIntent(t *testing.T) {
	q := testFileQueue(t)
	id := "0-1-0-regression"
	source := filepath.Join(q.dlqPath, id)
	data, _ := json.Marshal(types.Message{ID: "stable", Message: json.RawMessage(`{}`)})
	require.NoError(t, os.WriteFile(source, data, 0600))
	require.NoError(t, os.Mkdir(filepath.Join(q.path, id)+writingSuffix, 0700))
	require.Error(t, q.Recover(id, false))
	require.FileExists(t, source)
	require.FileExists(t, source+".replay")
	require.NoError(t, os.Remove(filepath.Join(q.path, id)+writingSuffix))
	require.NoError(t, q.Recover(id, false))
	require.EqualValues(t, 1, q.Len())
}

func TestCorruptDeadLetterCanBeInspectedButNotReplayed(t *testing.T) {
	q := testFileQueue(t)
	id := "0-1-0-regression"
	source := filepath.Join(q.dlqPath, id)
	require.NoError(t, os.WriteFile(source, []byte(`broken private response body`), 0600))
	records, err := q.Failed(0, 50)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Contains(t, records[0].Error, "Corrupt")
	require.NotContains(t, records[0].Error, "private response")
	require.Error(t, q.Recover(id, false))
	require.FileExists(t, source)
}

func TestConcurrentCrawlRecoveryAndStaleAcknowledgment(t *testing.T) {
	q, err := NewCrawlQueue(CrawlQueueParams{Path: filepath.Join(t.TempDir(), "crawl"), CrawlID: "concurrent", MaxAttempts: 1})
	require.NoError(t, err)
	s := q.(*crawlQueue)
	defer s.Close()
	msg := crawlRequest(t, "http://example.test/")
	require.NoError(t, s.Put(msg, 0))
	require.NoError(t, s.Start())
	msg = receiveCrawl(t, s)
	require.NoError(t, s.EndTransaction(msg, false))
	var wg sync.WaitGroup
	failures := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- s.Recover("http://example.test/", false) }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	retry := receiveCrawl(t, s)
	require.NoError(t, s.EndTransaction(retry, true))
	require.NoError(t, s.Recover("http://example.test/", true))
	recrawl := receiveCrawl(t, s)
	require.Error(t, s.EndTransaction(retry, true))
	require.NotEqual(t, retry.ID, recrawl.ID)
	require.NoError(t, s.EndTransaction(recrawl, true))
}
