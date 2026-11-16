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

func BenchmarkCrawlDispatch(b *testing.B) {
	for _, history := range []int{0, 1000, 10000} {
		b.Run(fmt.Sprint(history), func(b *testing.B) {
			q, err := NewCrawlQueue(CrawlQueueParams{Path: filepath.Join(b.TempDir(), "crawl"), CrawlID: "bench", MaxAttempts: 3})
			if err != nil {
				b.Fatal(err)
			}
			s := q.(*crawlQueue)
			defer s.Close()
			err = s.db.Update(func(txn *badger.Txn) error {
				for i := 0; i < history; i++ {
					data, _ := json.Marshal(crawlRecord{State: "completed"})
					if err := s.setRecord(txn, []byte(fmt.Sprintf("bench\x00http://example.test/%d", i)), data); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				b.Fatal(err)
			}
			if err = s.Start(); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				data, _ := json.Marshal(message.FetcherRequest{URI: fmt.Sprintf("http://pending.test/%d", i)})
				if err = s.Put(types.Message{ID: fmt.Sprint(i), MessageType: types.FetcherRequestType, Message: data}, 0); err != nil {
					b.Fatal(err)
				}
				select {
				case msg := <-s.GetChannel():
					if err = s.EndTransaction(msg, true); err != nil {
						b.Fatal(err)
					}
				case <-time.After(time.Second):
					b.Fatal("dispatch timeout")
				}
			}
		})
	}
}
