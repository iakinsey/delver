package frontier

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
)

func admissionStore(t *testing.T, p StoreParams) *Store {
	t.Helper()
	p.Path = filepath.Join(t.TempDir(), "frontier")
	s, err := OpenStore(p)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return s
}
func admissionVersion(s *Store) uint64 {
	tx := s.db.NewTransaction(false)
	defer tx.Discard()
	return tx.ReadTs()
}
func admissionRequests(n int, host string) []message.FetcherRequest {
	rows := make([]message.FetcherRequest, n)
	for i := range rows {
		uri := fmt.Sprintf("http://new-%d.test/page", i)
		if host != "" {
			uri = fmt.Sprintf("http://%s/page/%d", host, i)
		}
		rows[i] = message.FetcherRequest{URI: uri}
	}
	return rows
}

func TestDiscoveryFullCapacityIsReadOnly(t *testing.T) {
	s := admissionStore(t, StoreParams{MaxPending: 1})
	ctx := context.Background()
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://held.test/"}))
	require.NoError(t, s.StageDiscovery(ctx, "backlog", admissionRequests(4096, "")))
	before := admissionVersion(s)
	for i := 0; i < 100; i++ {
		n, err := s.DrainDiscovery(ctx, 4096)
		require.NoError(t, err)
		require.Zero(t, n)
		require.Equal(t, before, admissionVersion(s), "full-capacity polling committed a write")
	}
	counts, err := s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 4096, counts["deferred_discoveries"])
	require.EqualValues(t, 1, counts["pending_and_active"])
}

func TestDiscoveryStopsAtAvailableCapacity(t *testing.T) {
	s := admissionStore(t, StoreParams{MaxPending: 2})
	ctx := context.Background()
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://held.test/"}))
	require.NoError(t, s.StageDiscovery(ctx, "backlog", admissionRequests(4096, "")))
	before := admissionVersion(s)
	n, err := s.DrainDiscovery(ctx, 4096)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.LessOrEqual(t, admissionVersion(s)-before, uint64(3), "one free slot caused per-record writes")
	counts, err := s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 2, counts["pending_and_active"])
	require.EqualValues(t, 4095, counts["deferred_discoveries"])
}

func TestDiscoveryFullHostCheckpointsOnce(t *testing.T) {
	s := admissionStore(t, StoreParams{MaxPending: 10000, MaxPendingPerOrigin: 1})
	ctx := context.Background()
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://hot.test/held"}))
	require.NoError(t, s.StageDiscovery(ctx, "hot", admissionRequests(4096, "hot.test")))
	before := admissionVersion(s)
	n, err := s.DrainDiscovery(ctx, 256)
	require.NoError(t, err)
	require.Zero(t, n)
	require.LessOrEqual(t, admissionVersion(s)-before, uint64(1), "full-host skips committed per-record writes")
	counts, err := s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 4096, counts["deferred_discoveries"])
	require.EqualValues(t, 256, counts["capacity_deferrals"])
}

func TestDiscoveryHostCursorProgressAndRecovery(t *testing.T) {
	s := admissionStore(t, StoreParams{MaxPending: 10000, MaxPendingPerOrigin: 1})
	ctx := context.Background()
	held := message.FetcherRequest{URI: "http://hot.test/held"}
	require.NoError(t, s.Admit(ctx, held))
	requests := append(admissionRequests(16, "hot.test"), message.FetcherRequest{URI: "http://eligible.test/page"})
	require.NoError(t, s.StageDiscovery(ctx, "ordered", requests))
	for i := 0; i < 5; i++ {
		_, err := s.DrainDiscovery(ctx, 4)
		require.NoError(t, err)
	}
	rows, _, err := s.Pending(ctx, "", 10)
	require.NoError(t, err)
	require.Len(t, rows, 2, "eligible host starved behind the blocked host")
	var original Candidate
	for _, row := range rows {
		if row.Request.URI == held.URI {
			original = row
		}
	}
	require.NotEmpty(t, original.Request.RequestID)
	require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: original.Request, Success: true, HTTPCode: 200}))
	n, err := s.DrainDiscovery(ctx, 4)
	require.NoError(t, err)
	require.Equal(t, 1, n, "released host capacity was not recognized on the next pass")
	counts, err := s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 2, counts["pending_and_active"])
	require.EqualValues(t, 15, counts["deferred_discoveries"])
}

func TestDiscoveryKnownURLsAtFullHost(t *testing.T) {
	s := admissionStore(t, StoreParams{MaxPending: 16, MaxPendingPerOrigin: 2})
	ctx := context.Background()
	for _, uri := range []string{"http://hot.test/a", "http://hot.test/b"} {
		require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: uri, Depth: 4}))
	}
	require.NoError(t, s.StageDiscovery(ctx, "paths", []message.FetcherRequest{
		{URI: "http://hot.test/a", Depth: 2}, {URI: "http://hot.test/b", Depth: 4},
		{URI: "http://hot.test/new"}, {URI: "http://other.test/new"},
	}))
	n, err := s.DrainDiscovery(ctx, 4)
	require.NoError(t, err)
	require.Equal(t, 3, n)
	counts, err := s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 3, counts["admitted"], "duplicate/path updates consumed capacity")
	require.EqualValues(t, 1, counts["deferred_discoveries"])
	var candidate Candidate
	require.NoError(t, s.db.View(func(tx *badger.Txn) error { return read(tx, "u/http://hot.test/a", &candidate) }))
	require.Equal(t, 2, candidate.Request.Depth)
}

func TestDiscoveryStalePassCannotRewindCursor(t *testing.T) {
	s := admissionStore(t, StoreParams{MaxPending: 100, MaxPendingPerOrigin: 1})
	ctx := context.Background()
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://hot.test/held"}))
	require.NoError(t, s.StageDiscovery(ctx, "cursor", admissionRequests(16, "hot.test")))
	_, err := s.DrainDiscovery(ctx, 4)
	require.NoError(t, err)
	var first, second string
	require.NoError(t, s.db.View(func(tx *badger.Txn) error { return read(tx, "meta/staged_cursor", &first) }))
	_, err = s.DrainDiscovery(ctx, 4)
	require.NoError(t, err)
	require.NoError(t, s.db.View(func(tx *badger.Txn) error { return read(tx, "meta/staged_cursor", &second) }))
	require.NotEqual(t, first, second)
	before := admissionVersion(s)
	require.NoError(t, s.checkpointDiscovery(ctx, nil, "", first, 0))
	var current string
	require.NoError(t, s.db.View(func(tx *badger.Txn) error { return read(tx, "meta/staged_cursor", &current) }))
	require.Equal(t, second, current)
	require.Equal(t, before, admissionVersion(s))
}

func TestDiscoveryConcurrentCapacityAndReplay(t *testing.T) {
	s := admissionStore(t, StoreParams{MaxPending: 16, MaxPendingPerOrigin: 2})
	ctx := context.Background()
	requests := admissionRequests(256, "")
	for i := range requests {
		requests[i].URI = fmt.Sprintf("http://host-%d.test/%d", i%16, i)
	}
	require.NoError(t, s.StageDiscovery(ctx, "concurrent", requests))
	var wg sync.WaitGroup
	errors := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.DrainDiscovery(ctx, 32); errors <- err }()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	counts, err := s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 16, counts["pending_and_active"])
	require.EqualValues(t, 240, counts["deferred_discoveries"])
	require.NoError(t, s.Close())
	s, err = OpenStore(s.params)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	require.NoError(t, s.StageDiscovery(ctx, "concurrent", requests))
	for i := 0; i < 256; i++ {
		rows, _, err := s.Pending(ctx, "", 32)
		require.NoError(t, err)
		for _, row := range rows {
			require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: row.Request, Success: true, HTTPCode: 200}))
		}
		_, err = s.DrainDiscovery(ctx, 32)
		require.NoError(t, err)
		counts, err = s.Counts()
		require.NoError(t, err)
		if counts["pending_and_active"] == 0 && counts["deferred_discoveries"] == 0 {
			break
		}
	}
	require.EqualValues(t, 0, counts["pending_and_active"])
	require.EqualValues(t, 0, counts["deferred_discoveries"])
	require.EqualValues(t, 256, counts["admitted"])
	require.EqualValues(t, 256, counts["terminal_outcomes"])
}

func TestDiscoveryCleanupFailureAndRestart(t *testing.T) {
	s := admissionStore(t, StoreParams{MaxPending: 8})
	ctx := context.Background()
	require.NoError(t, s.StageDiscovery(ctx, "fault", admissionRequests(2, "")))
	marker := fmt.Sprintf("discovery/%x/%08d", sha256.Sum256([]byte("fault")), 0)
	// Fail staging cleanup after admission commits, then recover that boundary.
	require.NoError(t, s.db.Update(func(tx *badger.Txn) error { return write(tx, marker, 0) }))
	_, err := s.DrainDiscovery(ctx, 2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "discovery chunk count")
	counts, err := s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 2, counts["admitted"])
	require.EqualValues(t, 2, counts["deferred_discoveries"], "cleanup error partially deleted staged rows")
	require.NoError(t, s.Close())
	s, err = OpenStore(s.params)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	require.NoError(t, s.db.Update(func(tx *badger.Txn) error { return write(tx, marker, 2) }))
	_, err = s.DrainDiscovery(ctx, 2)
	require.NoError(t, err)
	counts, err = s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 2, counts["admitted"])
	require.Zero(t, counts["deferred_discoveries"])
}
