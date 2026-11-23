package queue

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"runtime"
	"testing"
)

func cacheFixtureURI(i int) string {
	path := make([]byte, 0, 2048)
	for n := 0; n < 32; n++ {
		sum := sha256.Sum256([]byte(fmt.Sprintf("fixture-%d-%d", i, n)))
		path = hex.AppendEncode(path, sum[:])
	}
	return "http://fixture.invalid/" + string(path)
}
func seedCacheFixture(b *testing.B, path string, count int) {
	b.Helper()
	q, e := NewCrawlQueue(CrawlQueueParams{Path: path, CrawlID: "fixture", MaxAttempts: 3})
	if e != nil {
		b.Fatal(e)
	}
	s := q.(*crawlQueue)
	batch := s.db.NewWriteBatch()
	defer batch.Cancel()
	for i := 0; i < count; i++ {
		uri := cacheFixtureURI(i)
		raw, _ := json.Marshal(message.FetcherRequest{URI: uri})
		data, _ := json.Marshal(crawlRecord{State: "completed", Message: types.Message{ID: fmt.Sprint(i), MessageType: types.FetcherRequestType, Message: raw}})
		if e = batch.Set([]byte("fixture\x00"+uri), data); e != nil {
			b.Fatal(e)
		}
	}
	if e = batch.Flush(); e != nil {
		b.Fatal(e)
	}
	if e = s.db.Flatten(1); e != nil {
		b.Fatal(e)
	}
	if e = s.Close(); e != nil {
		b.Fatal(e)
	}
}
func BenchmarkRequestQueueCacheWorkingSet(b *testing.B) {
	var cpu int64
	var peakHeap uint64
	observeHeap := func() {
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		peakHeap = max(peakHeap, memory.HeapAlloc)
	}
	for iteration := 0; iteration < b.N; iteration++ {
		b.StopTimer()
		path := b.TempDir()
		const count = 100000
		seedCacheFixture(b, path, count)
		runtime.GC()
		q, e := NewCrawlQueue(CrawlQueueParams{Path: path, CrawlID: "fixture", MaxAttempts: 3})
		if e != nil {
			b.Fatal(e)
		}
		s := q.(*crawlQueue)
		keys := make([][]byte, count)
		for i := range keys {
			keys[i] = []byte("fixture\x00" + cacheFixtureURI((i*7919)%count))
		}
		runtime.GC()
		observeHeap()
		cpuStart := benchmarkCPUTime()
		b.StartTimer()
		e = s.db.View(func(tx *badger.Txn) error {
			for i, key := range keys {
				if i%1000 == 0 {
					observeHeap()
				}
				item, e := tx.Get(key)
				if e != nil {
					return e
				}
				if e = item.Value(func(raw []byte) error {
					if len(raw) == 0 {
						return fmt.Errorf("empty record")
					}
					return nil
				}); e != nil {
					return e
				}
			}
			return nil
		})
		b.StopTimer()
		cpu += benchmarkCPUTime() - cpuStart
		observeHeap()
		if e != nil {
			b.Fatal(e)
		}
		keys = nil
		runtime.GC()
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		b.ReportMetric(float64(mem.HeapAlloc), "live-heap-bytes")
		if e = s.Close(); e != nil {
			b.Fatal(e)
		}
	}
	b.ReportMetric(float64(cpu)/float64(b.N), "cpu-ns/op")
	b.ReportMetric(float64(peakHeap), "read-peak-heap-bytes")
}
