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
	for i, id := range []string{"one", "two"} {
		r := message.FetcherRequest{URI: "https://a.test/", RequestID: types.UUID(id), FrontierGeneration: types.UUID(id), Depth: 4 - i*3}
		b, _ := json.Marshal(r)
		if err := q.Put(types.Message{ID: id, MessageType: types.FetcherRequestType, Message: b}, 0); err != nil {
			t.Fatal(err)
		}
		select {
		case m := <-q.GetChannel():
			if m.ID != id {
				t.Fatal(m.ID)
			}
			var delivered message.FetcherRequest
			if err := json.Unmarshal(m.Message, &delivered); err != nil {
				t.Fatal(err)
			}
			if delivered.RequestID != r.RequestID || delivered.FrontierGeneration != r.FrontierGeneration {
				t.Fatalf("generation identity changed: %+v", delivered)
			}
			if err := q.EndTransaction(m, true); err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("dispatch timeout")
		}
	}
}

func TestNewGenerationCannotReplaceLeasedBetterPath(t *testing.T) {
	q, err := NewCrawlQueue(CrawlQueueParams{Path: filepath.Join(t.TempDir(), "db"), CrawlID: "leased", MaxAttempts: 3, AcceptFrontierGenerations: true})
	if err != nil {
		t.Fatal(err)
	}
	defer q.(interface{ Close() error }).Close()
	if err = q.Start(); err != nil {
		t.Fatal(err)
	}
	r := message.FetcherRequest{URI: "https://a.test/", RequestID: "one", FrontierGeneration: "one", Depth: 4}
	body, _ := json.Marshal(r)
	if err = q.Put(types.Message{ID: "one", MessageType: types.FetcherRequestType, Message: body}, 0); err != nil {
		t.Fatal(err)
	}
	first := receiveCrawl(t, q)
	r.RequestID, r.FrontierGeneration, r.Depth = "two", "two", 1
	body, _ = json.Marshal(r)
	if err = q.Put(types.Message{ID: "two", MessageType: types.FetcherRequestType, Message: body}, 0); err == nil {
		t.Fatal("new generation overwrote an active delivery")
	}
	if err = q.EndTransaction(first, true); err != nil {
		t.Fatal(err)
	}
	if err = q.Put(types.Message{ID: "two", MessageType: types.FetcherRequestType, Message: body}, 0); err != nil {
		t.Fatal(err)
	}
	next := receiveCrawl(t, q)
	var delivered message.FetcherRequest
	if err = json.Unmarshal(next.Message, &delivered); err != nil {
		t.Fatal(err)
	}
	if delivered.RequestID != "two" {
		t.Fatalf("new request identity lost: %+v", delivered)
	}
	if err = q.EndTransaction(next, true); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareRepairsOnlyConfirmedFrontierIdentity(t *testing.T) {
	for _, name := range []string{"mismatch", "valid_retry", "different_generation", "not_dispatched", "failed"} {
		t.Run(name, func(t *testing.T) {
			q, err := NewCrawlQueue(CrawlQueueParams{Path: filepath.Join(t.TempDir(), "db"), CrawlID: "repair", MaxAttempts: 3, AcceptFrontierGenerations: true})
			if err != nil {
				t.Fatal(err)
			}
			s := q.(*crawlQueue)
			defer s.Close()
			r := message.FetcherRequest{URI: "https://a.test/", RequestID: "old", FrontierGeneration: "generation"}
			body, _ := json.Marshal(r)
			m := types.Message{ID: "old", MessageType: types.FetcherRequestType, Message: body}
			if err = q.Put(m, 0); err != nil {
				t.Fatal(err)
			}
			key, _ := s.key(m)
			state := "completed"
			if name == "failed" {
				state = "failed"
			}
			if err = s.db.Update(func(tx *badger.Txn) error {
				b, _ := json.Marshal(crawlRecord{Message: m, State: state, Attempts: 2})
				return tx.Set(key, b)
			}); err != nil {
				t.Fatal(err)
			}
			if err = s.SaveOutputs(m, []types.Message{{ID: "old-output", Message: json.RawMessage(`{}`)}}); err != nil {
				t.Fatal(err)
			}
			s.SetFrontierIdentityLookup(func(uri string) (message.FetcherRequest, bool, error) {
				current := r
				current.RequestID = "current"
				if name == "valid_retry" {
					current.RequestID = r.RequestID
				}
				if name == "different_generation" {
					current.FrontierGeneration = "another"
				}
				return current, name != "not_dispatched", nil
			})
			if err = s.Prepare(); err != nil {
				t.Fatal(err)
			}
			var record crawlRecord
			if err = s.db.View(func(tx *badger.Txn) error {
				item, err := tx.Get(key)
				if err != nil {
					return err
				}
				return item.Value(func(b []byte) error { return json.Unmarshal(b, &record) })
			}); err != nil {
				t.Fatal(err)
			}
			var actual message.FetcherRequest
			json.Unmarshal(record.Message.Message, &actual)
			_, outputs, err := s.LoadOutputs(m)
			if err != nil {
				t.Fatal(err)
			}
			if name == "mismatch" {
				if record.State != "pending" || actual.RequestID != "current" || record.Message.ID != "current" || outputs || record.Attempts != 0 {
					t.Fatalf("repair failed: %+v %+v outputs=%v", record, actual, outputs)
				}
				if err = s.Prepare(); err != nil {
					t.Fatal(err)
				}
				if q.Len() != 1 {
					t.Fatal("repair lost or duplicated pending work")
				}
			} else if record.State != state || actual.RequestID != "old" || !outputs {
				t.Fatalf("unrelated record changed: %+v %+v outputs=%v", record, actual, outputs)
			}
		})
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
