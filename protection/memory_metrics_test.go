package protection

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestInspectionReservationCancellationAndExactlyOnceRelease(t *testing.T) {
	p, err := Open(t.TempDir() + "/policy.sqlite")
	require.NoError(t, err)
	defer p.Close()
	release, err := p.Reserve(context.Background(), 256<<20)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = p.Reserve(ctx, 1)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	s := p.MemorySnapshot()
	require.Equal(t, 1, s.Active)
	require.Zero(t, s.Waiters)
	require.EqualValues(t, 1, s.Waits)
	require.Positive(t, s.WaitNS)
	release()
	release()
	s = p.MemorySnapshot()
	require.Zero(t, s.ReservedBytes)
	require.Zero(t, s.Active)
	require.EqualValues(t, 256<<20, s.PeakBytes)
}
