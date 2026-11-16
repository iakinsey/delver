package frontier

import (
	"context"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func retryParams(path string) StoreParams {
	return StoreParams{Path: path, OriginDelay: Lifetime(time.Nanosecond), Retention: DefaultRetention(), Retry: &RetryPolicy{Share: .2, Limit: 1, Base: flow.Duration(50 * time.Millisecond), Maximum: flow.Duration(time.Second), FailureThreshold: 3, HostCooldown: flow.Duration(20 * time.Millisecond), HostCooldownMax: flow.Duration(time.Second), ProbeInterval: flow.Duration(50 * time.Millisecond)}}
}
func reserveURL(t *testing.T, s *Store, uri string) Candidate {
	t.Helper()
	require.NoError(t, s.Admit(context.Background(), message.FetcherRequest{URI: uri}))
	rows, err := s.Candidates(context.Background(), "breadth_first", "", 100)
	require.NoError(t, err)
	for _, c := range rows {
		if c.Request.URI == uri {
			c, err = s.Reserve(context.Background(), c)
			require.NoError(t, err)
			require.NoError(t, s.Published(c))
			return c
		}
	}
	t.Fatal("missing candidate")
	return Candidate{}
}
func TestRetrySurvivesRestartRetentionAndOutcomeReplay(t *testing.T) {
	p := retryParams(t.TempDir())
	s, err := OpenStore(p)
	require.NoError(t, err)
	c := reserveURL(t, s, "http://slow.example/a")
	r := message.FetcherResponse{FetcherRequest: c.Request, Retryable: true, FailureClass: "timeout"}
	require.NoError(t, s.Outcome(r))
	require.NoError(t, s.Outcome(r))
	counts, err := s.Counts()
	require.NoError(t, err)
	require.Equal(t, int64(1), counts["retry_pending"])
	require.Zero(t, counts["terminal_outcomes"])
	require.NoError(t, s.DrainRetries(context.Background(), 10))
	rows, err := s.Candidates(context.Background(), "breadth_first", "", 10)
	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoError(t, s.Close())
	s, err = OpenStore(p)
	require.NoError(t, err)
	defer s.Close()
	_, err = s.Cleanup(context.Background(), time.Now().Add(365*24*time.Hour))
	require.NoError(t, err)
	time.Sleep(60 * time.Millisecond)
	require.NoError(t, s.DrainRetries(context.Background(), 10))
	rows, err = s.Candidates(context.Background(), "breadth_first", "", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	next := rows[0]
	require.Equal(t, 1, next.Request.Attempt)
	require.NotEqual(t, c.Request.RequestID, next.Request.RequestID)
	require.NotEqual(t, c.Request.FrontierGeneration, next.Request.FrontierGeneration)
	require.NoError(t, s.Outcome(r))
	counts, _ = s.Counts()
	require.Equal(t, int64(1), counts["pending_and_active"])
	next, err = s.Reserve(context.Background(), next)
	require.NoError(t, err)
	require.NoError(t, s.Published(next))
	require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: next.Request, Success: true, HTTPCode: 200}))
	counts, _ = s.Counts()
	require.Equal(t, int64(1), counts["terminal_outcomes"])
	require.Zero(t, counts["retry_pending"])
	require.Zero(t, counts["pending_and_active"])
}
func TestHostCooldownAdmitsSingleRecoveryProbe(t *testing.T) {
	p := retryParams(t.TempDir())
	p.Retry.Limit = 0
	s, err := OpenStore(p)
	require.NoError(t, err)
	defer s.Close()
	for _, path := range []string{"a", "b", "c"} {
		c := reserveURL(t, s, "http://dead.example/"+path)
		require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: c.Request, Retryable: true, FailureClass: "connection"}))
	}
	require.NoError(t, s.Admit(context.Background(), message.FetcherRequest{URI: "http://dead.example/d"}))
	require.NoError(t, s.Admit(context.Background(), message.FetcherRequest{URI: "http://dead.example/e"}))
	rows, err := s.Candidates(context.Background(), "breadth_first", "", 10)
	require.NoError(t, err)
	_, err = s.Reserve(context.Background(), rows[0])
	require.Equal(t, ErrCooling, err)
	time.Sleep(30 * time.Millisecond)
	probe, err := s.Reserve(context.Background(), rows[0])
	require.NoError(t, err)
	_, err = s.Reserve(context.Background(), rows[1])
	require.Equal(t, ErrCooling, err)
	require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: probe.Request, Success: true, HTTPCode: 200}))
	_, err = s.Reserve(context.Background(), rows[1])
	require.NoError(t, err)
}
func TestPolicyFailuresDoNotRetryAndRetryAfterIsRespected(t *testing.T) {
	s, err := OpenStore(retryParams(t.TempDir()))
	require.NoError(t, err)
	defer s.Close()
	c := reserveURL(t, s, "http://one.example/a")
	require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: c.Request, FailureClass: "policy"}))
	counts, _ := s.Counts()
	require.Zero(t, counts["retries_scheduled"])
	c = reserveURL(t, s, "http://two.example/a")
	require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: c.Request, Retryable: true, RetryAfterMS: 1000}))
	require.NoError(t, s.DrainRetries(context.Background(), 10))
	rows, _, err := s.Pending(context.Background(), "", 10)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestReadyRetriesRemainVisibleBehindLargeFreshFrontier(t *testing.T) {
	p := retryParams(t.TempDir())
	s, err := OpenStore(p)
	require.NoError(t, err)
	defer s.Close()
	for i := 0; i < 40; i++ {
		require.NoError(t, s.Admit(context.Background(), message.FetcherRequest{URI: fmt.Sprintf("http://fresh-%d.example/", i)}))
	}
	c := reserveURL(t, s, "http://retry.example/")
	require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: c.Request, Retryable: true, FailureClass: "timeout"}))
	time.Sleep(60 * time.Millisecond)
	require.NoError(t, s.DrainRetries(context.Background(), 10))
	for _, strategy := range []string{"breadth_first", "depth_first", "host_diverse", "adaptive"} {
		rows, err := s.Candidates(context.Background(), strategy, "", 5)
		require.NoError(t, err)
		retries := 0
		for _, row := range rows {
			if row.Request.Attempt > 0 {
				retries++
			}
		}
		require.Greater(t, retries, 0, strategy)
		require.Len(t, rows, 5)
	}
}

func TestFreshWorkCannotBeHiddenByLargeRetryBacklogAndIndexesUpgrade(t *testing.T) {
	p := retryParams(t.TempDir())
	p.Retry.FailureThreshold = 1000
	s, err := OpenStore(p)
	require.NoError(t, err)
	for i := 0; i < 20; i++ {
		c := reserveURL(t, s, fmt.Sprintf("http://retry-%d.example/", i))
		require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: c.Request, Retryable: true, FailureClass: "timeout"}))
	}
	for i := 0; i < 5; i++ {
		require.NoError(t, s.Admit(context.Background(), message.FetcherRequest{URI: fmt.Sprintf("http://fresh-%d.example/", i)}))
	}
	time.Sleep(60 * time.Millisecond)
	require.NoError(t, s.DrainRetries(context.Background(), 100))
	verify := func() {
		for _, strategy := range []string{"breadth_first", "depth_first", "host_diverse", "adaptive"} {
			rows, err := s.Candidates(context.Background(), strategy, "", 5)
			require.NoError(t, err)
			fresh := 0
			for _, c := range rows {
				if c.Request.Attempt == 0 {
					fresh++
				}
			}
			require.Equal(t, 4, fresh, strategy)
			require.Len(t, rows, 5)
		}
	}
	verify()
	require.NoError(t, s.db.Update(func(tx *badger.Txn) error {
		for _, prefix := range []string{"fb/", "fd/", "fh/"} {
			it := tx.NewIterator(badger.DefaultIteratorOptions)
			for it.Seek([]byte(prefix)); it.ValidForPrefix([]byte(prefix)); it.Next() {
				if err := tx.Delete(it.Item().KeyCopy(nil)); err != nil {
					it.Close()
					return err
				}
			}
			it.Close()
		}
		return tx.Delete([]byte("meta/fresh_index_v1"))
	}))
	require.NoError(t, s.Close())
	s, err = OpenStore(p)
	require.NoError(t, err)
	defer s.Close()
	verify()
}
