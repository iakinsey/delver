package frontier

import (
	"context"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestScanContinuesBeyondCoolingWindow(t *testing.T) {
	for _, strategy := range []string{"breadth_first", "depth_first", "host_diverse", "adaptive"} {
		t.Run(strategy, func(t *testing.T) {
			s, err := OpenStore(StoreParams{Path: t.TempDir(), MaxPending: 6000})
			require.NoError(t, err)
			defer s.Close()
			ctx := context.Background()
			for i := 0; i < 4100; i++ {
				require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: fmt.Sprintf("http://a%05d.test/x", i)}))
			}
			require.NoError(t, s.update(func(tx *badger.Txn) error {
				for i := 0; i < 4100; i++ {
					// Leave the record ordered last by each strategy eligible.
					if strategy == "depth_first" && i == 0 || strategy != "depth_first" && i == 4099 {
						continue
					}
					if err := write(tx, fmt.Sprintf("cool/a%05d.test", i), time.Now().Add(time.Hour).UnixNano()); err != nil {
						return err
					}
				}
				return nil
			}))
			legacy, err := s.Candidates(ctx, strategy, "", 4096)
			require.NoError(t, err)
			require.Len(t, legacy, 4096)
			for _, c := range legacy {
				_, err = s.Reserve(ctx, c)
				require.Equal(t, ErrCooling, err)
			}
			first, err := s.ScanCandidates(ctx, strategy, ScanCursor{}, 4096)
			require.NoError(t, err)
			require.Empty(t, first.Candidates)
			require.Equal(t, 4096, first.Inspected)
			require.False(t, first.Complete)
			second, err := s.ScanCandidates(ctx, strategy, first.Cursor, 4096)
			require.NoError(t, err)
			require.Len(t, second.Candidates, 1)
			require.LessOrEqual(t, second.Inspected, 4096)
			c, err := s.Reserve(ctx, second.Candidates[0])
			require.NoError(t, err)
			require.NoError(t, s.Published(c))
		})
	}
}

func TestScanDeletedCursorInsertionExpiryAndRestart(t *testing.T) {
	ctx := context.Background()
	p := StoreParams{Path: t.TempDir()}
	s, err := OpenStore(p)
	require.NoError(t, err)
	for i := 0; i < 12; i++ {
		require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: fmt.Sprintf("http://h%02d.test/x", i), Depth: 2}))
	}
	page, err := s.ScanCandidates(ctx, "breadth_first", ScanCursor{}, 4)
	require.NoError(t, err)
	require.Len(t, page.Candidates, 4)
	// Publishing deletes the exact key held by the scan cursor.
	for _, c := range page.Candidates {
		reserved, e := s.Reserve(ctx, c)
		require.NoError(t, e)
		require.NoError(t, s.Published(reserved))
		require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: reserved.Request, Success: true, HTTPCode: 200}))
	}
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://new.test/a", Depth: 1}))
	second, err := s.ScanCandidates(ctx, "breadth_first", page.Cursor, 4)
	require.NoError(t, err)
	require.Len(t, second.Candidates, 4)
	require.Equal(t, "http://h04.test/x", second.Candidates[0].Request.URI)
	reset, err := s.ScanCandidates(ctx, "breadth_first", ScanCursor{}, 4)
	require.NoError(t, err)
	require.Equal(t, "http://new.test/a", reset.Candidates[0].Request.URI)
	require.NoError(t, s.update(func(tx *badger.Txn) error { return write(tx, "cool/new.test", time.Now().Add(-time.Second).UnixNano()) }))
	require.NoError(t, s.Close())
	s, err = OpenStore(p)
	require.NoError(t, err)
	defer s.Close()
	recovered, err := s.ScanCandidates(ctx, "breadth_first", ScanCursor{}, 4)
	require.NoError(t, err)
	require.Equal(t, "http://new.test/a", recovered.Candidates[0].Request.URI)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.ScanCandidates(canceled, "breadth_first", ScanCursor{}, 4)
	require.Equal(t, context.Canceled, err)
}

func TestScanPreservesUnblockedTraversalOrder(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(StoreParams{Path: t.TempDir()})
	require.NoError(t, err)
	defer s.Close()
	for i, depth := range []int{2, 0, 1, 2, 1} {
		require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: fmt.Sprintf("http://h%d.test/x", i), Depth: depth}))
	}
	for _, strategy := range []string{"breadth_first", "depth_first"} {
		want, err := s.Candidates(ctx, strategy, "", 20)
		require.NoError(t, err)
		got, err := s.ScanCandidates(ctx, strategy, ScanCursor{}, 20)
		require.NoError(t, err)
		require.Equal(t, want, got.Candidates)
	}
}

func TestScanRetryAndReservedStreamsCannotHideFreshWork(t *testing.T) {
	ctx := context.Background()
	p := millionParams(t.TempDir(), 1000)
	s, err := OpenStore(p)
	require.NoError(t, err)
	defer s.Close()
	for i := 0; i < 80; i++ {
		require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: fmt.Sprintf("http://retry-%03d.test/x", i)}))
	}
	rows, err := s.Candidates(ctx, "breadth_first", "", 100)
	require.NoError(t, err)
	for i, c := range rows {
		c, err = s.Reserve(ctx, c)
		require.NoError(t, err)
		if i < 40 {
			require.NoError(t, s.Published(c))
			require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: c.Request, Retryable: true, FailureClass: "timeout"}))
		}
	}
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://fresh.test/x"}))
	time.Sleep(110 * time.Millisecond)
	require.NoError(t, s.DrainRetries(ctx, 100))
	require.NoError(t, s.update(func(tx *badger.Txn) error {
		for i := 0; i < 40; i++ {
			if err := write(tx, fmt.Sprintf("cool/retry-%03d.test", i), time.Now().Add(time.Hour).UnixNano()); err != nil {
				return err
			}
		}
		return nil
	}))
	cursor := ScanCursor{}
	found := false
	for pass := 0; pass < 10 && !found; pass++ {
		page, err := s.ScanCandidates(ctx, "breadth_first", cursor, 16)
		require.NoError(t, err)
		require.LessOrEqual(t, page.Inspected, 16)
		for _, c := range page.Candidates {
			if c.Request.URI == "http://fresh.test/x" {
				found = true
			}
		}
		cursor = page.Cursor
	}
	require.True(t, found, "recovery stream hid fresh work across bounded passes")
}
