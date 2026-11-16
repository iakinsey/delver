package flow

import (
	"context"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMetricsPersistAcrossRestartWithBoundedRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.json")
	c, err := New(context.Background(), t.Name(), testPolicy(), nil, path)
	require.NoError(t, err)
	c.Record(200, true, true, "", time.Millisecond, 42, 40)
	c.Counter("storage_operations", 3)
	c.Close()
	c, err = New(context.Background(), t.Name(), testPolicy(), nil, path)
	require.NoError(t, err)
	require.Equal(t, int64(1), c.Snapshot().Totals.Attempts)
	require.Equal(t, int64(3), c.Snapshot().Counters["storage_operations"])
	c.Close()
	require.NoError(t, os.WriteFile(path, []byte(`{"errors":{"unbounded":99,"timeout":2},"statuses":{"9999":99,"200":4},"counters":{"unknown":99,"storage_operations":8}}`), 0600))
	c, err = New(context.Background(), t.Name(), testPolicy(), nil, path)
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"timeout": 2}, c.Snapshot().Errors)
	require.Equal(t, map[int]int64{200: 4}, c.Snapshot().HTTPStatuses)
	require.Equal(t, map[string]int64{"storage_operations": 8}, c.Snapshot().Counters)
	c.Close()
	require.NoError(t, os.WriteFile(path, make([]byte, 65537), 0600))
	c, err = New(context.Background(), t.Name(), testPolicy(), nil, path)
	require.NoError(t, err)
	require.Contains(t, c.Snapshot().LastAdjustment, "could not be restored")
	c.Close()
}

func TestOrderlyCloseCheckpointsAttemptsFinishingAfterParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	path := filepath.Join(t.TempDir(), "metrics.json")
	c, err := New(ctx, t.Name(), testPolicy(), nil, path)
	require.NoError(t, err)
	c.Record(200, true, true, "", time.Millisecond, 1, 1)
	cancel()
	<-c.done
	// An admitted worker may finish after cancellation has stopped sampling, but
	// before the application's worker shutdown has reached Controller.Close.
	c.Record(0, false, false, "cancelled", time.Millisecond, 0, 0)
	c.Close()
	restored, err := New(context.Background(), t.Name()+"-restored", testPolicy(), nil, path)
	require.NoError(t, err)
	defer restored.Close()
	require.Equal(t, int64(2), restored.Snapshot().Totals.Attempts)
}
