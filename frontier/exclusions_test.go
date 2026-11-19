package frontier

import (
	"context"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestExcludedPendingAndReservedReleaseCapacityAndCanBeRediscovered(t *testing.T) {
	ctx := context.Background()
	p := StoreParams{Path: t.TempDir(), OriginDelay: Lifetime(time.Nanosecond), RevisitAfter: Lifetime(time.Hour)}
	s, err := OpenStore(p)
	require.NoError(t, err)
	for _, uri := range []string{"http://a.test/pending.pdf", "http://b.test/reserved.pdf", "http://c.test/page"} {
		require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: uri}))
	}
	cs, err := s.Candidates(ctx, "breadth_first", "", 10)
	require.NoError(t, err)
	reserved, err := s.Reserve(ctx, cs[1])
	require.NoError(t, err)
	require.Equal(t, "reserved", reserved.State)
	require.NoError(t, s.Close())
	p.ExcludedExtensions = []string{"pdf"}
	s, err = OpenStore(p)
	require.NoError(t, err)
	page, err := s.ScanCandidates(ctx, "breadth_first", ScanCursor{}, 32)
	require.NoError(t, err)
	require.Len(t, page.Excluded, 2)
	require.Len(t, page.Candidates, 1)
	for _, c := range page.Excluded {
		require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: c.Request, FailureClass: "excluded"}))
	}
	n, err := s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 1, n["pending_and_active"])
	require.Zero(t, n["reserved"])
	require.Zero(t, n["failed_fetches"])
	require.Zero(t, n["retry_pending"])
	require.EqualValues(t, 2, n["policy_skips"])
	require.NoError(t, s.Close())
	p.ExcludedExtensions = nil
	s, err = OpenStore(p)
	require.NoError(t, err)
	defer s.Close()
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://a.test/pending.pdf"}))
	n, err = s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 2, n["pending_and_active"])
}

func TestExcludedDelayedRetryExpiresWithoutVisitedMarker(t *testing.T) {
	ctx := context.Background()
	p := millionParams(t.TempDir(), 1000)
	p.RevisitAfter = Lifetime(time.Hour)
	s, err := OpenStore(p)
	require.NoError(t, err)
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://retry.test/file.pdf"}))
	rows, err := s.Candidates(ctx, "breadth_first", "", 1)
	require.NoError(t, err)
	c, err := s.Reserve(ctx, rows[0])
	require.NoError(t, err)
	require.NoError(t, s.Published(c))
	require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: c.Request, Retryable: true, FailureClass: "timeout"}))
	require.NoError(t, s.Close())
	p.ExcludedExtensions = []string{"pdf"}
	s, err = OpenStore(p)
	require.NoError(t, err)
	defer s.Close()
	time.Sleep(110 * time.Millisecond)
	require.NoError(t, s.DrainRetries(ctx, 8))
	page, err := s.ScanCandidates(ctx, "breadth_first", ScanCursor{}, 32)
	require.NoError(t, err)
	require.Len(t, page.Excluded, 1)
	require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: page.Excluded[0].Request, FailureClass: "excluded", Retryable: true}))
	counts, err := s.Counts()
	require.NoError(t, err)
	require.Zero(t, counts["retry_pending"])
	require.Zero(t, counts["pending_and_active"])
	require.Zero(t, counts["failed_fetches"])
	require.EqualValues(t, 1, counts["retries_scheduled"])
	_, err = s.Cleanup(ctx, time.Now().Add(10*time.Second))
	require.NoError(t, err)
	require.NoError(t, s.view(func(tx *badger.Txn) error {
		_, err := tx.Get([]byte("u/" + c.Request.URI))
		require.Equal(t, badger.ErrKeyNotFound, err)
		return nil
	}))
}

func TestConcurrentExcludedAdmissionsKeepExactSkipCounts(t *testing.T) {
	s, err := OpenStore(StoreParams{Path: t.TempDir(), ExcludedExtensions: []string{"pdf"}})
	require.NoError(t, err)
	defer s.Close()
	var wg sync.WaitGroup
	failures := make(chan error, 16)
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 64; i++ {
				if err := s.Admit(context.Background(), message.FetcherRequest{URI: fmt.Sprintf("http://example.test/%d-%d.pdf", worker, i)}); err != nil {
					failures <- err
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	counts, err := s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 1024, counts["policy_skips"])
	require.Zero(t, counts["pending_and_active"])
}

func TestExcludedRedirectStaysDeduplicatedUntilPolicyChanges(t *testing.T) {
	ctx := context.Background()
	p := StoreParams{Path: t.TempDir(), ExcludedExtensions: []string{"pdf", "zip"}, OriginDelay: Lifetime(time.Nanosecond)}
	s, err := OpenStore(p)
	require.NoError(t, err)
	defer func() { s.Close() }()
	uri := "http://example.test/redirect"
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: uri}))
	rows, err := s.Candidates(ctx, "breadth_first", "", 1)
	require.NoError(t, err)
	old, err := s.Reserve(ctx, rows[0])
	require.NoError(t, err)
	require.NoError(t, s.Published(old))
	require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: old.Request, FailureClass: "excluded"}))
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: uri}))
	counts, err := s.Counts()
	require.NoError(t, err)
	require.Zero(t, counts["pending_and_active"], "unchanged policy repeatedly admits the same blocked redirect")
	require.NoError(t, s.Close())
	p.ExcludedExtensions = []string{"ZIP", ".PDF"}
	s, err = OpenStore(p)
	require.NoError(t, err)
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: uri}))
	counts, err = s.Counts()
	require.NoError(t, err)
	require.Zero(t, counts["pending_and_active"], "rule reordering changed policy identity")
	require.NoError(t, s.Close())
	p.ExcludedExtensions = nil
	s, err = OpenStore(p)
	require.NoError(t, err)
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: uri}))
	rows, err = s.Candidates(ctx, "breadth_first", "", 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotEqual(t, old.Request.FrontierGeneration, rows[0].Request.FrontierGeneration)
}
