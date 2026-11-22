package frontier

import (
	"context"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/trap"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestPurgeFrontierIndexesStagingTrapEvidenceAndRediscovery(t *testing.T) {
	ctx := context.Background()
	settings := trap.Default()
	root := t.TempDir()
	s, e := OpenStore(StoreParams{Path: filepath.Join(root, "frontier"), Traps: &settings})
	require.NoError(t, e)
	defer s.Close()
	for _, uri := range []string{"http://bad.test/page", "http://good.test/page"} {
		require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: uri}))
	}
	require.NoError(t, s.StageDiscovery(ctx, "synthetic-stage", []message.FetcherRequest{{URI: "http://bad.test/staged"}, {URI: "http://good.test/staged", Origin: "http://bad.test/page"}}))
	_, e = s.ObserveTrap(ctx, corpusObservation(t, 150, 2, 0, 0, true))
	require.NoError(t, e)
	// Evidence belonging only to the restricted site must not leave a template
	// family or stale LSH pointers after purge.
	signature := make([]uint64, 64)
	for n := range signature {
		signature[n] = uint64(n + 1)
	}
	_, e = s.ObserveTrap(ctx, trap.Observation{URI: "http://bad.test/page", Site: "bad.test", Signature: signature, Tokens: 50, Links: 12})
	require.NoError(t, e)
	require.NoError(t, s.db.Update(func(tx *badger.Txn) error { return write(tx, "meta/park_cursor", "trap/parked/bad.test/") }))
	p, e := protection.Open(filepath.Join(root, "policy.sqlite"))
	require.NoError(t, e)
	defer p.Close()
	_, e = p.Trigger("http://bad.test/", "synthetic", "crawl", "run", 0)
	require.ErrorIs(t, e, protection.ErrBlocked)
	s.SetProtection(p)
	match := protection.NewMatcher(nil, []protection.Target{{Site: "bad.test"}}).Matches
	require.Error(t, s.VerifyNoMatches(ctx, match))
	require.NoError(t, s.PurgeMatching(ctx, match))
	require.NoError(t, s.VerifyNoMatches(ctx, match))
	require.NoError(t, s.PurgeMatching(ctx, match))
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://bad.test/new"}))
	require.NoError(t, s.StageDiscovery(ctx, "rediscover", []message.FetcherRequest{{URI: "http://bad.test/new"}}))
	require.NoError(t, s.VerifyNoMatches(ctx, match))
	rows, e := s.Candidates(ctx, "breadth_first", "", 10)
	require.NoError(t, e)
	require.Len(t, rows, 1)
	require.Equal(t, "http://good.test/page", rows[0].Request.URI)
}

func TestTrapRestoreCannotOverrideRestrictedQuarantine(t *testing.T) {
	root := t.TempDir()
	s, e := OpenStore(StoreParams{Path: filepath.Join(root, "db")})
	require.NoError(t, e)
	defer s.Close()
	require.NoError(t, s.db.Update(func(tx *badger.Txn) error { return write(tx, "trap/blocked/bad.test", TrapDecision{Site: "bad.test"}) }))
	p, e := protection.Open(filepath.Join(root, "policy.sqlite"))
	require.NoError(t, e)
	defer p.Close()
	_, e = p.Trigger("http://bad.test/", "synthetic", "crawl", "run", 0)
	require.ErrorIs(t, e, protection.ErrBlocked)
	s.SetProtection(p)
	require.ErrorIs(t, s.RestoreTrap(context.Background(), "bad.test"), protection.ErrBlocked)
}

func TestPurgeRemovesParkedDiscoveriesAndTheirReceipts(t *testing.T) {
	ctx := context.Background()
	store, e := OpenStore(StoreParams{Path: filepath.Join(t.TempDir(), "frontier")})
	require.NoError(t, e)
	defer store.Close()
	require.NoError(t, store.StageDiscovery(ctx, "parked-source", []message.FetcherRequest{{URI: "http://blocked.test/a"}, {URI: "http://blocked.test/b"}, {URI: "http://good.test/page", Origin: "http://blocked.test/from"}}))
	require.NoError(t, store.db.Update(func(tx *badger.Txn) error {
		return write(tx, "trap/blocked/blocked.test", TrapDecision{Site: "blocked.test"})
	}))
	_, e = store.DrainDiscovery(ctx, 128)
	require.NoError(t, e)
	counts, e := store.Counts()
	require.NoError(t, e)
	require.EqualValues(t, 2, counts["trap_parked"])
	require.Zero(t, counts["deferred_discoveries"])
	match := protection.NewMatcher(nil, []protection.Target{{Site: "blocked.test"}}).Matches
	require.NoError(t, store.PurgeMatching(ctx, match))
	require.NoError(t, store.VerifyNoMatches(ctx, match))
	require.NoError(t, store.PurgeMatching(ctx, match))
	counts, e = store.Counts()
	require.NoError(t, e)
	require.Zero(t, counts["trap_parked"])
	require.EqualValues(t, 1, counts["pending_and_active"])
	require.NoError(t, store.db.View(func(tx *badger.Txn) error {
		options := badger.DefaultIteratorOptions
		options.PrefetchValues = false
		it := tx.NewIterator(options)
		defer it.Close()
		it.Seek([]byte("discovery/"))
		require.False(t, it.ValidForPrefix([]byte("discovery/")), "purged receipt remains")
		return nil
	}))
}
