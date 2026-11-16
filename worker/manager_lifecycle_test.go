package worker

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type blockingWorker struct {
	entered   chan struct{}
	release   chan struct{}
	mu        sync.Mutex
	completed int
}

func (s *blockingWorker) OnMessage(types.Message) (interface{}, error) {
	s.entered <- struct{}{}
	<-s.release
	return nil, nil
}
func (s *blockingWorker) OnComplete() { s.mu.Lock(); defer s.mu.Unlock(); s.completed++ }
func TestManagerWaitsForPool(t *testing.T) {
	input := queue.NewChannelQueue()
	worker := &blockingWorker{entered: make(chan struct{}, 3), release: make(chan struct{})}
	manager := NewWorkerManager(worker, input, nil)
	for i := 0; i < 3; i++ {
		go manager.Start()
	}
	for i := 0; i < 3; i++ {
		if err := input.Put(types.Message{}, 0); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		<-worker.entered
	}
	done := make(chan struct{})
	go func() { manager.Stop(); manager.Stop(); close(done) }()
	select {
	case <-done:
		t.Fatal("closed worker before active calls finished")
	case <-time.After(20 * time.Millisecond):
	}
	close(worker.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("pool failed to stop")
	}
	if worker.completed != 1 {
		t.Fatal("completion called multiple times")
	}
}

type replayWorker struct{ calls int }

func (s *replayWorker) OnMessage(types.Message) (interface{}, error) {
	s.calls++
	return types.MultiMessage{Values: []interface{}{types.Message{Message: json.RawMessage(`1`)}, types.Message{Message: json.RawMessage(`2`)}}}, nil
}
func (*replayWorker) OnComplete() {}
func TestPersistentOutputReplay(t *testing.T) {
	root := t.TempDir()
	input := queue.NewFileQueue(queue.FileQueueParams{Name: "replay", Path: filepath.Join(root, "in"), DlqPath: filepath.Join(root, "dlq"), MaxPollDelayMs: 1, Reset: true})
	input.Put(types.Message{Message: json.RawMessage(`{}`)}, 0)
	input.Start()
	defer input.Stop()
	var first types.Message
	select {
	case first = <-input.GetChannel():
	case <-time.After(time.Second):
		t.Fatal("no input")
	}
	worker := &replayWorker{}
	manager := NewWorkerManager(worker, input, nil).(*workerManager)
	manager.outbox = queue.NewChannelQueue()
	outputs, err := manager.process(first)
	if err != nil {
		t.Fatal(err)
	}
	if err = input.EndTransaction(first, false); err != nil {
		t.Fatal(err)
	}
	var retry types.Message
	select {
	case retry = <-input.GetChannel():
	case <-time.After(time.Second):
		t.Fatal("no retry")
	}
	replayed, err := manager.process(retry)
	if err != nil {
		t.Fatal(err)
	}
	if worker.calls != 1 || len(replayed) != 2 || outputs[0].ID != replayed[0].ID {
		t.Fatal("output batch recomputed or identity changed")
	}
	input.EndTransaction(retry, true)
}
func TestChannelPublicationCancellation(t *testing.T) {
	q := queue.NewChannelQueue()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := q.(interface {
		PutContext(context.Context, types.Message, int) error
	}).PutContext(ctx, types.Message{}, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
