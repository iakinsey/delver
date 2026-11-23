package flow

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMappedMemoryBlocksAdmissionAndReleasesExactlyOnce(t *testing.T) {
	start := mappedMemory.Load()
	policy := Default()
	policy.MemoryHighMB = 64
	policy.MemoryLowMB = 32
	controller, err := New(context.Background(), "mapped-memory-fixture", policy, nil)
	require.NoError(t, err)
	defer controller.Close()
	release := TrackMappedMemory(80 << 20)
	defer release()
	require.EqualValues(t, start+(80<<20), mappedMemory.Load())
	controller.Evaluate(time.Now(), Pressure{MemoryMB: PressureMemoryMB(), FetchPending: 100})
	require.Contains(t, controller.Snapshot().Reason, "memory")
	release()
	release()
	require.Equal(t, start, mappedMemory.Load())
	controller.Evaluate(time.Now().Add(time.Second), Pressure{MemoryMB: 0, FetchPending: 100})
	require.NotContains(t, controller.Snapshot().Reason, "memory")
}
