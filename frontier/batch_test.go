package frontier

import (
	"context"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestOversizedRetryAndCleanupBatchesRemainAtomicAndBounded(t *testing.T) {
	p := retryParams(t.TempDir())
	p.CacheBudgetMB = 32
	p.Retention.CompletedFor = Lifetime(time.Millisecond)
	p.Retention.FailedFor = Lifetime(time.Millisecond)
	p.Retention.DedupFor = Lifetime(2 * time.Millisecond)
	p.Retention.CleanupBatchSize = 4096
	s, err := OpenStore(p)
	require.NoError(t, err)
	defer s.Close()
	const n = 2048
	original := map[string]Candidate{}
	ctx := context.Background()
	for i := 0; i < n; i++ {
		uri := fmt.Sprintf("http://batch-%d.invalid/%s", i, strings.Repeat("x", 1024))
		require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: uri}))
		var c Candidate
		require.NoError(t, s.db.View(func(tx *badger.Txn) error { return read(tx, "u/"+uri, &c) }))
		c, err = s.Reserve(ctx, c)
		require.NoError(t, err)
		require.NoError(t, s.Published(c))
		original[uri] = c
		require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: c.Request, Retryable: true, FailureClass: "timeout"}))
	}
	time.Sleep(60 * time.Millisecond)
	require.NoError(t, s.DrainRetries(ctx, 4096))
	counts, err := s.Counts()
	require.NoError(t, err)
	require.Zero(t, counts["retry_pending"])
	require.Equal(t, int64(n), counts["retries_scheduled"])
	require.NoError(t, s.DrainRetries(ctx, 4096))
	for uri, old := range original {
		var c Candidate
		require.NoError(t, s.db.View(func(tx *badger.Txn) error { return read(tx, "u/"+uri, &c) }))
		require.Equal(t, 1, c.Request.Attempt)
		require.NotEqual(t, old.Request.RequestID, c.Request.RequestID)
		c, err = s.Reserve(ctx, c)
		require.NoError(t, err)
		require.NoError(t, s.Published(c))
		require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: c.Request, Success: true, HTTPCode: 200}))
	}
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://protected.invalid/"}))
	total := 0
	for {
		cleaned, e := s.Cleanup(ctx, time.Now().Add(time.Hour))
		require.NoError(t, e)
		require.LessOrEqual(t, cleaned, p.Retention.CleanupBatchSize)
		total += cleaned
		if cleaned == 0 {
			break
		}
	}
	require.Equal(t, n*2, total)
	counts, err = s.Counts()
	require.NoError(t, err)
	require.Equal(t, int64(1), counts["pending_and_active"])
	require.Equal(t, int64(n), counts["terminal_outcomes"])
	require.Equal(t, int64(n*2), counts["cleaned"])
	rows, _, err := s.Pending(ctx, "", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "http://protected.invalid/", rows[0].Request.URI)
	require.Empty(t, s.Health())
}
