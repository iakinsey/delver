package frontier

import (
	"context"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/repetition"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// Extend the existing empty-storage million lifecycle with a legacy mixed
// provenance cohort. Test setup changes only origins; depth, sequence, run
// identity, receipts and the lifecycle's original assertions remain intact.
func repeatedMillionUpgrade(t *testing.T, s *Store, params StoreParams, n int, emit func(string, map[string]interface{})) (*Store, StoreParams) {
	t.Helper()
	ctx := context.Background()
	started := time.Now()
	for start := 0; start < n; start += 128 {
		end := min(n, start+128)
		require.NoError(t, s.update(func(tx *badger.Txn) error {
			for i := start; i < end; i++ {
				if i%2 != 0 {
					continue
				}
				uri := millionURI(i)
				var c Candidate
				if err := read(tx, "u/"+uri, &c); err != nil {
					return err
				}
				c.Request.Origin = "http://mirror-0.test/"
				if err := write(tx, "u/"+uri, c); err != nil {
					return err
				}
			}
			return nil
		}))
	}
	require.NoError(t, s.Close())
	p := repetition.Default()
	params.RepeatedPages = &p
	opened, err := OpenStore(params)
	require.NoError(t, err)
	family := confirmRepeatedFixture(t, opened)
	for i := 0; i < 5; i++ {
		require.NoError(t, opened.MaintainRepeatedRouting(ctx, 128))
	}
	before, err := opened.RepeatedPages(ctx, "", "", 1)
	require.NoError(t, err)
	require.NoError(t, opened.Close())
	opened, err = OpenStore(params)
	require.NoError(t, err)
	after, err := opened.RepeatedPages(ctx, "", "", 1)
	require.NoError(t, err)
	require.Equal(t, before.Routing.Cursor, after.Routing.Cursor)
	maintainRepeated(t, opened)
	require.Equal(t, n/2, millionKeys(t, opened, "frb/"))
	require.Equal(t, (n+1)/2, millionKeys(t, opened, "fb/"))
	for _, strategy := range []string{"breadth_first", "depth_first", "host_diverse", "adaptive"} {
		page, err := opened.ScanCandidates(ctx, strategy, ScanCursor{}, 100)
		require.NoError(t, err)
		require.Len(t, page.Candidates, 100)
		repeated := 0
		for _, c := range page.Candidates {
			if c.Repeated {
				repeated++
			}
		}
		require.Equal(t, 10, repeated)
	}
	require.NoError(t, opened.SetFamilyExempt(ctx, family, true))
	maintainRepeated(t, opened)
	require.Zero(t, millionKeys(t, opened, "frb/"))
	require.Equal(t, n, millionKeys(t, opened, "fb/"))
	require.NoError(t, opened.SetFamilyExempt(ctx, family, false))
	maintainRepeated(t, opened)
	require.Equal(t, n/2, millionKeys(t, opened, "frb/"))
	counts, err := opened.Counts()
	require.NoError(t, err)
	require.EqualValues(t, n, counts["pending_and_active"])
	require.EqualValues(t, n, counts["admitted"])
	emit("repeated_legacy_upgrade_and_reclassification", map[string]interface{}{"urls": n, "repeated": n / 2, "seconds": time.Since(started).Seconds(), "restart_cursor": fmt.Sprint(before.Routing.Cursor)})
	return opened, params
}
