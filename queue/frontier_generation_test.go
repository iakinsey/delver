package queue

import (
	"encoding/json"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"path/filepath"
	"testing"
	"time"
)

func TestFrontierGeneration(t *testing.T) {
	q, err := NewCrawlQueue(CrawlQueueParams{Path: filepath.Join(t.TempDir(), "db"), CrawlID: "test", MaxAttempts: 3, NotifyDispatch: true, AcceptFrontierGenerations: true})
	if err != nil {
		t.Fatal(err)
	}
	defer q.(interface{ Close() error }).Close()
	if err := q.Start(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		r := message.FetcherRequest{URI: "https://a.test/", RequestID: types.UUID(id), FrontierGeneration: types.UUID(id)}
		b, _ := json.Marshal(r)
		if err := q.Put(types.Message{ID: id, MessageType: types.FetcherRequestType, Message: b}, 0); err != nil {
			t.Fatal(err)
		}
		select {
		case m := <-q.GetChannel():
			if m.ID != id {
				t.Fatal(m.ID)
			}
			if err := q.EndTransaction(m, true); err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("dispatch timeout")
		}
	}
}

func TestLegacyCrawlIndexesAndCounts(t *testing.T) {
	p := CrawlQueueParams{Path: filepath.Join(t.TempDir(), "db"), CrawlID: "legacy", MaxAttempts: 3, NotifyDispatch: true}
	q, err := NewCrawlQueue(p)
	if err != nil {
		t.Fatal(err)
	}
	s := q.(*crawlQueue)
	if err := s.db.Update(func(tx *badger.Txn) error {
		for i := 0; i < 2; i++ {
			r := message.FetcherRequest{URI: fmt.Sprintf("https://a.test/%d", i)}
			body, _ := json.Marshal(r)
			record, _ := json.Marshal(crawlRecord{Message: types.Message{ID: fmt.Sprint(i), MessageType: types.FetcherRequestType, Message: body}, State: "pending"})
			if err := tx.Set([]byte("legacy\x00"+r.URI), record); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if q.Len() != 2 {
		t.Fatal(q.Len())
	}
	if err := q.Start(); err != nil {
		t.Fatal(err)
	}
	var m types.Message
	select {
	case m = <-q.GetChannel():
	case <-time.After(time.Second):
		t.Fatal("dispatch timeout")
	}
	if err := q.EndTransaction(m, true); err != nil {
		t.Fatal(err)
	}
	if q.Len() != 1 {
		t.Fatal(q.Len())
	}
	counts, err := s.Counts()
	if err != nil || counts["completed"] != 1 || counts["pending"]+counts["leased"] != 1 {
		t.Fatal(counts, err)
	}
	if _, err := s.ReferencedObjects(); err != nil {
		t.Fatal(err)
	}
	var r message.FetcherRequest
	json.Unmarshal(m.Message, &r)
	if err := s.Recover(r.URI, true); err != nil {
		t.Fatal(err)
	}
	if q.Len() != 2 {
		t.Fatal(q.Len())
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	q, err = NewCrawlQueue(p)
	if err != nil {
		t.Fatal(err)
	}
	s = q.(*crawlQueue)
	defer s.Close()
	if q.Len() != 2 {
		t.Fatal(q.Len())
	}
	if err := q.Start(); err != nil {
		t.Fatal(err)
	}
	counts, err = s.Counts()
	if err != nil || counts["pending"]+counts["leased"] != 2 || counts["completed"] != 0 {
		t.Fatal(counts, err)
	}
}
