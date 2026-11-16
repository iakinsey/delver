package worker

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/stretchr/testify/require"
)

func TestFullOutboxRetainsStagedInputForRecovery(t *testing.T) {
	root := t.TempDir()
	params := queue.FileQueueParams{Name: "input", Path: filepath.Join(root, "input"), DlqPath: filepath.Join(root, "failed"), MaxAttempts: 1, MaxPollDelayMs: 1, Reset: true, Resilient: true}
	input := queue.NewFileQueue(params)
	outbox := queue.NewFileQueue(queue.FileQueueParams{Name: "output", Path: filepath.Join(root, "output"), DlqPath: filepath.Join(root, "out-failed"), MaxSize: 1, Resilient: true})
	require.NoError(t, outbox.Put(types.Message{ID: "blocker", Message: json.RawMessage(`{}`)}, 0))
	require.NoError(t, input.Put(types.Message{ID: "original", Message: json.RawMessage(`{}`)}, 0))
	require.NoError(t, input.Start())
	worker := &replayWorker{}
	manager := NewWorkerManager(worker, input, outbox)
	go manager.Start()
	recovery := input.(queue.Recovery)
	var failed []queue.FailedRecord
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var err error
		failed, err = recovery.Failed(0, 50)
		require.NoError(t, err)
		if len(failed) > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	manager.Stop()
	require.NoError(t, input.Stop())
	require.Len(t, failed, 1)
	require.Equal(t, 1, worker.calls)
	require.Contains(t, failed[0].Error, "capacity")
	resumed := queue.NewFileQueue(params)
	require.NoError(t, resumed.(queue.Recovery).Recover(failed[0].ID, false))
	require.NoError(t, resumed.Start())
	defer resumed.Stop()
	select {
	case msg := <-resumed.GetChannel():
		outputs, found, err := resumed.(queue.OutputStore).LoadOutputs(msg)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, outputs, 2)
		require.Equal(t, "original", msg.ID)
		require.NoError(t, resumed.EndTransaction(msg, true))
	case <-time.After(time.Second):
		t.Fatal("recoverable input was lost")
	}
	require.EqualValues(t, 1, outbox.Len())
}
