package application

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/worker"
	"github.com/stretchr/testify/require"
)

type pauseTestWorker struct{ processed atomic.Int32 }

func (w *pauseTestWorker) OnMessage(types.Message) (interface{}, error) {
	w.processed.Add(1)
	return nil, nil
}
func (*pauseTestWorker) OnComplete() {}

// Pending durable work must not be drained to pause, even when execution
// admission cannot grant a single permit. Resume must process it exactly once.
func TestRuntimePauseWithBlockedDurableBacklog(t *testing.T) {
	for _, kind := range []string{"file", "crawl"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			var q queue.Queue
			if kind == "file" {
				q = queue.NewFileQueue(queue.FileQueueParams{Name: "pause", Path: filepath.Join(root, "in"), DlqPath: filepath.Join(root, "dlq"), MaxAttempts: 3, MaxPollDelayMs: 1})
			} else {
				var err error
				q, err = queue.NewCrawlQueue(queue.CrawlQueueParams{Path: filepath.Join(root, "in"), CrawlID: "pause", MaxAttempts: 3})
				require.NoError(t, err)
			}
			defer q.Stop()
			if closer, ok := q.(interface{ Close() error }); ok {
				defer closer.Close()
			}
			var allowed atomic.Bool
			var permits atomic.Int32
			entered := make(chan struct{}, 1)
			q.(interface {
				SetDispatchGate(func(context.Context) (func(), error))
			}).SetDispatchGate(func(ctx context.Context) (func(), error) {
				select {
				case entered <- struct{}{}:
				default:
				}
				for !allowed.Load() {
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(time.Millisecond):
					}
				}
				permits.Add(1)
				return func() { permits.Add(-1) }, nil
			})
			for i := 0; i < 20; i++ {
				data, err := json.Marshal(message.FetcherRequest{URI: fmt.Sprintf("http://pause.test/%d", i), RequestID: types.NewV4()})
				require.NoError(t, err)
				require.NoError(t, q.Put(types.Message{ID: string(types.NewV4()), MessageType: types.FetcherRequestType, Message: data}, 0))
			}
			w := &pauseTestWorker{}
			m := worker.NewWorkerManager(w, q, nil)
			prepared := &preparedApplication{app: config.Application{Workers: []config.Worker{{Name: "consumer", Inbox: "in"}}}, workers: map[string]worker.WorkerManager{"consumer": m}, resources: map[string]interface{}{"in": q}}
			c := &runtimeControl{prepared: prepared}
			require.NoError(t, q.Start())
			go m.Start()
			defer m.Stop()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("admission not reached")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, c.pause(ctx, true))
			require.EqualValues(t, 20, q.Len())
			require.Zero(t, w.processed.Load())
			require.Zero(t, permits.Load())
			// A grant completing after pause must be returned without leasing a record.
			allowed.Store(true)
			time.Sleep(50 * time.Millisecond)
			require.Zero(t, permits.Load())
			require.EqualValues(t, 20, q.Len())
			require.NoError(t, c.pause(ctx, false))
			require.Eventually(t, func() bool { return w.processed.Load() == 20 && q.Len() == 0 && permits.Load() == 0 }, 3*time.Second, time.Millisecond)
			require.NoError(t, c.pause(ctx, true))
			require.EqualValues(t, 20, w.processed.Load())
		})
	}
}
