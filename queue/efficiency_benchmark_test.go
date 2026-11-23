package queue

import (
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func BenchmarkFileQueueSelection(b *testing.B) {
	for _, count := range []int{0, 64, 10000, 100000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			root := b.TempDir()
			q := NewFileQueue(FileQueueParams{Name: "fixture", Path: root + "/pending", DlqPath: root + "/failed", MaxAttempts: 3, MaxPollDelayMs: 20}).(*fileQueue)
			payload, _ := json.Marshal(types.Message{ID: "fixture", Message: []byte(`{"uri":"http://fixture.invalid/"}`)})
			for i := 0; i < count; i++ {
				name := fmt.Sprintf("%d-%d-%d-fixture", i%11, int64(1700000000000000000)+int64(i), i)
				if e := os.WriteFile(filepath.Join(q.path, name), payload, 0600); e != nil {
					b.Fatal(e)
				}
			}
			b.ReportAllocs()
			cpuStart := benchmarkCPUTime()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if count > 0 && i > 0 && i%count == 0 {
					b.StopTimer()
					for n := 0; n < count; n++ {
						name := fmt.Sprintf("%d-%d-%d-fixture", n%11, int64(1700000000000000000)+int64(n), n)
						if e := os.WriteFile(filepath.Join(q.path, name), payload, 0600); e != nil {
							b.Fatal(e)
						}
					}
					if e := q.Prepare(); e != nil {
						b.Fatal(e)
					}
					b.StartTimer()
				}
				m, e := q.next()
				if count == 0 {
					if e != errQueueEmpty {
						b.Fatal(e)
					}
					continue
				}
				if e != nil {
					b.Fatal(e)
				}
				if e = q.EndTransaction(*m, true); e != nil {
					b.Fatal(e)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(benchmarkCPUTime()-cpuStart)/float64(b.N), "cpu-ns/op")
		})
	}
}

func BenchmarkRequestQueueResources(b *testing.B) {
	for _, count := range []int{0, 10000, 100000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			var live, peak uint64
			var cpu int64
			for iteration := 0; iteration < b.N; iteration++ {
				b.StopTimer()
				root := filepath.Join(b.TempDir(), fmt.Sprint(iteration))
				q, e := NewCrawlQueue(CrawlQueueParams{Path: root, CrawlID: "bench", MaxAttempts: 3, NotifyDispatch: true})
				if e != nil {
					b.Fatal(e)
				}
				s := q.(*crawlQueue)
				batch := s.db.NewWriteBatch()
				for i := 0; i < count; i++ {
					uri := fmt.Sprintf("http://history.invalid/%s/%d", strings.Repeat("fixture-", 64), i)
					raw, _ := json.Marshal(message.FetcherRequest{URI: uri})
					value, _ := json.Marshal(crawlRecord{State: "completed", Message: types.Message{ID: fmt.Sprint(i), MessageType: types.FetcherRequestType, Message: raw}})
					if e = batch.Set([]byte("bench\x00"+uri), value); e != nil {
						b.Fatal(e)
					}
				}
				if e = batch.Flush(); e != nil {
					b.Fatal(e)
				}
				batch.Cancel()
				if count > 0 {
					if e = s.db.Flatten(1); e != nil {
						b.Fatal(e)
					}
				}
				cpuStart := benchmarkCPUTime()
				b.StartTimer()
				if e = s.Start(); e != nil {
					b.Fatal(e)
				}
				for i := 0; i < 32; i++ {
					raw, _ := json.Marshal(message.FetcherRequest{URI: fmt.Sprintf("http://pending.invalid/%d", i)})
					if e = s.Put(types.Message{ID: fmt.Sprint(i), MessageType: types.FetcherRequestType, Message: raw}, 0); e != nil {
						b.Fatal(e)
					}
					select {
					case m := <-s.GetChannel():
						if e = s.EndTransaction(m, true); e != nil {
							b.Fatal(e)
						}
					case <-time.After(10 * time.Second):
						b.Fatal("dispatch stalled")
					}
				}
				b.StopTimer()
				cpu += benchmarkCPUTime() - cpuStart
				runtime.GC()
				var stats runtime.MemStats
				runtime.ReadMemStats(&stats)
				live = max(live, stats.HeapAlloc)
				var usage unix.Rusage
				_ = unix.Getrusage(unix.RUSAGE_SELF, &usage)
				peak = max(peak, uint64(usage.Maxrss))
				if e = s.Close(); e != nil {
					b.Fatal(e)
				}
			}
			b.ReportMetric(float64(cpu)/float64(b.N), "cpu-ns/op")
			b.ReportMetric(float64(live), "live-heap-bytes")
			b.ReportMetric(float64(peak), "peak-rss-KiB")
		})
	}
}

func BenchmarkFileQueuePump(b *testing.B) {
	for iteration := 0; iteration < b.N; iteration++ {
		b.StopTimer()
		root := b.TempDir()
		q := NewFileQueue(FileQueueParams{Name: "fixture", Path: root + "/pending", DlqPath: root + "/failed", MaxAttempts: 3, MaxPollDelayMs: 20}).(*fileQueue)
		const count = 1000
		for i := 0; i < count; i++ {
			if e := q.Put(types.Message{Message: []byte(`{"fixture":true}`)}, 0); e != nil {
				b.Fatal(e)
			}
		}
		b.StartTimer()
		if e := q.Start(); e != nil {
			b.Fatal(e)
		}
		for i := 0; i < count; i++ {
			select {
			case m := <-q.GetChannel():
				if e := q.EndTransaction(m, true); e != nil {
					b.Fatal(e)
				}
			case <-time.After(30 * time.Second):
				q.Stop()
				b.Fatal("queue pump stalled")
			}
		}
		if e := q.Stop(); e != nil {
			b.Fatal(e)
		}
		b.StopTimer()
	}
}

func benchmarkCPUTime() int64 {
	var r unix.Rusage
	_ = unix.Getrusage(unix.RUSAGE_SELF, &r)
	return r.Utime.Nano() + r.Stime.Nano()
}
