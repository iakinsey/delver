package queue

import (
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"path/filepath"
	"testing"
)

func BenchmarkCrawlDispatchModes(b *testing.B) {
	for _, notify := range []bool{false, true} {
		b.Run(fmt.Sprintf("notify_%t", notify), func(b *testing.B) {
			q, err := NewCrawlQueue(CrawlQueueParams{Path: filepath.Join(b.TempDir(), "db"), CrawlID: "bench", MaxAttempts: 3, NotifyDispatch: notify})
			if err != nil {
				b.Fatal(err)
			}
			defer q.(interface{ Close() error }).Close()
			if err := q.Start(); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r := message.FetcherRequest{URI: fmt.Sprintf("https://bench.test/%d", i)}
				body, _ := json.Marshal(r)
				if err := q.Put(types.Message{ID: fmt.Sprint(i), MessageType: types.FetcherRequestType, Message: body}, 0); err != nil {
					b.Fatal(err)
				}
				m := <-q.GetChannel()
				if err := q.EndTransaction(m, true); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
