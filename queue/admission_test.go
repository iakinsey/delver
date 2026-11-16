package queue

import (
	"context"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdmissionPrecedesLeaseAndReleasesOnAck(t *testing.T) {
	for _, kind := range []string{"crawl", "file", "channel"} {
		t.Run(kind, func(t *testing.T) {
			var q Queue
			switch kind {
			case "crawl":
				var err error
				q, err = NewCrawlQueue(CrawlQueueParams{Path: filepath.Join(t.TempDir(), "crawl"), CrawlID: "admission", MaxAttempts: 3})
				require.NoError(t, err)
			case "file":
				root := t.TempDir()
				q = NewFileQueue(FileQueueParams{Name: "admission", Path: filepath.Join(root, "queue"), DlqPath: filepath.Join(root, "dlq"), MaxAttempts: 3, MaxPollDelayMs: 1, MaxSize: 100, Resilient: true})
			case "channel":
				q = NewChannelQueue()
			}
			var active int32
			entered := make(chan struct{}, 1)
			allow := make(chan struct{})
			q.(interface {
				SetDispatchGate(func(context.Context) (func(), error))
			}).SetDispatchGate(func(ctx context.Context) (func(), error) {
				select {
				case entered <- struct{}{}:
				default:
				}
				select {
				case <-allow:
					atomic.AddInt32(&active, 1)
					return func() { atomic.AddInt32(&active, -1) }, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			})
			require.NoError(t, q.Prepare())
			require.NoError(t, q.Start())
			defer q.Stop()
			put := make(chan error, 1)
			go func() { put <- q.Put(crawlRequest(t, "http://admission.test/"), 0) }()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("gate not entered")
			}
			select {
			case <-q.GetChannel():
				t.Fatal("delivered before admission")
			default:
			}
			if counter, ok := q.(interface {
				Counts() (map[string]int64, error)
			}); ok {
				counts, err := counter.Counts()
				require.NoError(t, err)
				require.Zero(t, counts["leased"])
			}
			close(allow)
			message := receiveCrawl(t, q)
			require.NoError(t, <-put)
			require.Equal(t, int32(1), atomic.LoadInt32(&active))
			require.NoError(t, q.EndTransaction(message, true))
			require.Eventually(t, func() bool { return atomic.LoadInt32(&active) == 0 }, time.Second, time.Millisecond)
		})
	}
}
func TestChannelStopCancelsAdmission(t *testing.T) {
	q := NewChannelQueue()
	entered := make(chan struct{})
	q.(interface {
		SetDispatchGate(func(context.Context) (func(), error))
	}).SetDispatchGate(func(ctx context.Context) (func(), error) { close(entered); <-ctx.Done(); return nil, ctx.Err() })
	result := make(chan error, 1)
	go func() { result <- q.Put(crawlRequest(t, "http://cancel.test/"), 0) }()
	<-entered
	require.NoError(t, q.Stop())
	select {
	case err := <-result:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("admission did not cancel")
	}
}

func TestEmptyFileFetchQueueDoesNotRequestCapacity(t *testing.T) {
	root := t.TempDir()
	q := NewFileQueue(FileQueueParams{Name: "empty", Path: filepath.Join(root, "queue"), DlqPath: filepath.Join(root, "dlq"), MaxAttempts: 3, MaxPollDelayMs: 1, MaxSize: 100})
	var calls int32
	q.(interface {
		SetDispatchGate(func(context.Context) (func(), error))
	}).SetDispatchGate(func(context.Context) (func(), error) { atomic.AddInt32(&calls, 1); return func() {}, nil })
	require.NoError(t, q.Start())
	defer q.Stop()
	time.Sleep(30 * time.Millisecond)
	require.Zero(t, atomic.LoadInt32(&calls))
}

func TestPausedDispatchDrainsGrantedDeliveryAndCancelsWait(t *testing.T) {
	for _, kind := range []string{"file", "crawl"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			var q Queue
			if kind == "file" {
				q = NewFileQueue(FileQueueParams{Name: "pause", Path: filepath.Join(root, "in"), DlqPath: filepath.Join(root, "dlq"), MaxAttempts: 3, MaxPollDelayMs: 1})
			} else {
				var err error
				q, err = NewCrawlQueue(CrawlQueueParams{Path: filepath.Join(root, "in"), CrawlID: "pause", MaxAttempts: 3})
				require.NoError(t, err)
			}
			defer q.Stop()
			if closer, ok := q.(interface{ Close() error }); ok {
				defer closer.Close()
			}
			dispatch := q.(PausableDispatch)
			dispatch.PauseDispatch(true)
			require.NoError(t, q.Put(crawlRequest(t, "http://pause.test/first"), 0))
			require.NoError(t, q.Put(crawlRequest(t, "http://pause.test/second"), 0))
			require.NoError(t, q.Start())
			time.Sleep(30 * time.Millisecond)
			require.Zero(t, dispatch.DispatchActive())
			select {
			case <-q.GetChannel():
				t.Fatal("paused queue dispatched")
			default:
			}
			dispatch.PauseDispatch(false)
			first := receiveCrawl(t, q)
			dispatch.PauseDispatch(true)
			// A pump may already have granted one more delivery. Finish every grant;
			// records not granted must remain pending and must not acquire a lease.
			require.GreaterOrEqual(t, dispatch.DispatchActive(), 1)
			require.NoError(t, q.EndTransaction(first, true))
			deadline := time.After(time.Second)
			for dispatch.DispatchActive() > 0 {
				select {
				case next := <-q.GetChannel():
					require.NoError(t, q.EndTransaction(next, true))
				case <-deadline:
					t.Fatal("granted delivery stranded")
				}
			}
			pending := q.Len()
			time.Sleep(30 * time.Millisecond)
			require.Equal(t, pending, q.Len())
			require.Zero(t, dispatch.DispatchActive())
			stopped := make(chan error, 1)
			go func() { stopped <- q.Stop() }()
			select {
			case err := <-stopped:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("paused pump failed to stop")
			}
		})
	}
}
