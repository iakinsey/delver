package queue

import (
	"context"
	"fmt"
	"github.com/iakinsey/delver/types"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentCrawlProducersDispatchAndAcksStayHealthy(t *testing.T) {
	q, err := NewCrawlQueue(CrawlQueueParams{Path: filepath.Join(t.TempDir(), "requests"), CrawlID: "contention", MaxAttempts: 3, NotifyDispatch: true})
	require.NoError(t, err)
	require.NoError(t, q.Start())
	defer q.(interface{ Close() error }).Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const total = 1000
	var done int32
	var readers sync.WaitGroup
	for i := 0; i < 16; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case m := <-q.GetChannel():
					if e := q.(OutputStore).SaveOutputs(m, []types.Message{{ID: "out-" + m.ID}}); e != nil {
						t.Error(e)
						return
					}
					if e := q.EndTransaction(m, true); e != nil {
						t.Error(e)
						return
					}
					atomic.AddInt32(&done, 1)
				}
			}
		}()
	}
	var writers sync.WaitGroup
	for producer := 0; producer < 8; producer++ {
		writers.Add(1)
		go func(p int) {
			defer writers.Done()
			for i := p; i < total; i += 8 {
				if e := PutContext(ctx, q, crawlRequest(t, fmt.Sprintf("http://contention.test/%d", i)), 0); e != nil {
					t.Error(e)
					return
				}
			}
		}(producer)
	}
	writers.Wait()
	for atomic.LoadInt32(&done) < total && ctx.Err() == nil {
		require.Empty(t, q.(interface{ Health() string }).Health(), "transient contention must not latch queue health")
		time.Sleep(time.Millisecond)
	}
	require.Equal(t, int32(total), atomic.LoadInt32(&done))
	counts, err := q.(interface {
		Counts() (map[string]int64, error)
	}).Counts()
	require.NoError(t, err)
	require.Equal(t, int64(total), counts["completed"])
	require.Zero(t, counts["pending"])
	require.Zero(t, counts["leased"])
	require.Zero(t, counts["failed"])
	cancel()
	readers.Wait()
}
