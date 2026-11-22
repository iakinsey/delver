package frontier

import (
	"context"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
)

func TestRecrawlsDeferReadsUntilAdmissionCapacityExists(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(StoreParams{Path: t.TempDir(), MaxPending: 2, MaxPendingPerOrigin: 2, RevisitAfter: Lifetime(time.Hour)})
	require.NoError(t, err)
	defer s.Close()
	for _, uri := range []string{"http://work.test/one", "http://work.test/two"} {
		require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: uri}))
	}
	work, _, err := s.Pending(ctx, "", 2)
	require.NoError(t, err)
	require.Len(t, work, 2)
	first := revisitKey(time.Now().Add(-time.Hour).UnixNano(), "http://due.test/one")
	second := revisitKey(time.Now().Add(-time.Minute).UnixNano(), "http://due.test/two")
	require.NoError(t, s.update(func(tx *badger.Txn) error {
		if e := write(tx, first, message.FetcherRequest{URI: "http://due.test/one"}); e != nil {
			return e
		}
		// A later corrupt value proves that a refill only reads the rows needed
		// for available capacity. It must surface once that row can be admitted.
		return tx.Set([]byte(second), []byte("invalid JSON"))
	}))
	require.NoError(t, s.DrainRecrawls(ctx, 4096))
	require.NoError(t, s.view(func(tx *badger.Txn) error {
		_, e := tx.Get([]byte(first))
		require.NoError(t, e, "full admission must retain revisit ownership")
		return e
	}))
	require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: work[0].Request}))
	require.NoError(t, s.DrainRecrawls(ctx, 4096))
	counts, err := s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 2, counts["pending_and_active"])
	require.NoError(t, s.view(func(tx *badger.Txn) error {
		var c Candidate
		if e := read(tx, "u/http://due.test/one", &c); e != nil {
			return e
		}
		require.Equal(t, "pending", c.State)
		_, e := tx.Get([]byte(first))
		require.ErrorIs(t, e, badger.ErrKeyNotFound)
		return nil
	}))
	require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: work[1].Request}))
	require.Error(t, s.DrainRecrawls(ctx, 4096), "an eligible storage error must remain visible")
	require.NoError(t, s.update(func(tx *badger.Txn) error {
		return write(tx, second, message.FetcherRequest{URI: "http://due.test/two"})
	}))
	require.NoError(t, s.Close())
	s, err = OpenStore(StoreParams{Path: s.params.Path, MaxPending: 2, MaxPendingPerOrigin: 2, RevisitAfter: Lifetime(time.Hour)})
	require.NoError(t, err)
	defer s.Close()
	require.NoError(t, s.DrainRecrawls(ctx, 4096))
	require.NoError(t, s.view(func(tx *badger.Txn) error {
		var c Candidate
		if e := read(tx, "u/http://due.test/two", &c); e != nil {
			return e
		}
		require.Equal(t, "pending", c.State)
		_, e := tx.Get([]byte(second))
		require.ErrorIs(t, e, badger.ErrKeyNotFound)
		return nil
	}))
}
