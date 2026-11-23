package report

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func atlasFixture(t *testing.T, n int) (*Store, string, AtlasInfo) {
	t.Helper()
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "source.sqlite"), false)
	require.NoError(t, e)
	t.Cleanup(func() { s.Close() })
	tx, e := s.db.Begin()
	require.NoError(t, e)
	for i := 1; i <= n; i++ {
		_, e = tx.Exec("INSERT INTO hosts(id,name,known,visited) VALUES(?,?,?,?)", i, fmt.Sprintf("h%d.test", i), 3, 1)
		require.NoError(t, e)
		if i > 1 {
			_, e = tx.Exec("INSERT INTO edges VALUES(?,?)", i-1, i)
			require.NoError(t, e)
		}
	}
	_, e = tx.Exec("INSERT INTO hosts(id,name,known,visited) VALUES(?,?,?,?)", n+1, "unvisited.test", 1, 0)
	require.NoError(t, e)
	_, e = tx.Exec("INSERT INTO edges VALUES(?,?)", n, n+1)
	require.NoError(t, e)
	_, e = tx.Exec("INSERT INTO hosts(id,name,known,visited) VALUES(?,?,?,?)", n+2, "orphan.test", 1, 1)
	require.NoError(t, e)
	require.NoError(t, tx.Commit())
	path := filepath.Join(t.TempDir(), "atlas.sqlite")
	o := DefaultAtlasOptions()
	info, e := s.GenerateAtlas(ctx, path, "first", o, func(string) {})
	require.NoError(t, e)
	return s, path, info
}
func TestAtlasFilteringBoundsInspectionAndPersistence(t *testing.T) {
	_, path, info := atlasFixture(t, 140)
	ctx := context.Background()
	require.Equal(t, 140, info.Hosts)
	require.Equal(t, 139, info.Edges)
	read, e := ReadAtlasInfo(ctx, path)
	require.NoError(t, e)
	require.Equal(t, info.ID, read.ID)
	frame, e := AtlasView(ctx, path, AtlasViewport{Snapshot: info.ID, Bounds: []float64{-1, -1, 2, 2}, NodeLimit: 50, EdgeLimit: 100})
	require.NoError(t, e)
	require.True(t, frame.Rasterized)
	require.LessOrEqual(t, len(frame.Nodes), 50)
	require.LessOrEqual(t, len(frame.Edges), 100)
	require.Equal(t, 140, frame.Hosts)
	require.Equal(t, 139, frame.Links)
	for _, node := range frame.Nodes {
		require.NotZero(t, node.HostID)
		require.Contains(t, node.ID, "h:")
	}
	require.False(t, frame.Detailed)
	exact, e := AtlasView(ctx, path, AtlasViewport{Snapshot: info.ID, Bounds: []float64{-1, -1, 2, 2}, NodeLimit: 500, EdgeLimit: 500})
	require.NoError(t, e)
	require.False(t, exact.Grouped)
	require.Len(t, exact.Nodes, 140)
	require.Len(t, exact.Edges, 139)
	host, e := AtlasInspect(ctx, path, info.ID, 2, "incoming", 0)
	require.NoError(t, e)
	require.Equal(t, "h2.test", host.Host.Name)
	require.Equal(t, 1, host.Host.Incoming)
	require.Equal(t, 1, host.Host.Outgoing)
	require.EqualValues(t, 1, host.Neighbors[0].ID)
	host, e = AtlasInspect(ctx, path, info.ID, 2, "outgoing", 0)
	require.NoError(t, e)
	require.EqualValues(t, 3, host.Neighbors[0].ID)
	rows, e := AtlasSearch(ctx, path, "h2.test")
	require.NoError(t, e)
	require.Len(t, rows, 1)
	rows, e = AtlasSearch(ctx, path, "%")
	require.NoError(t, e)
	require.Empty(t, rows)
	_, e = AtlasView(ctx, path, AtlasViewport{Snapshot: "stale", Bounds: []float64{0, 0, 1, 1}, NodeLimit: 500, EdgeLimit: 500})
	require.Error(t, e)
	require.Contains(t, e.Error(), "snapshot changed")
}
func TestAtlasFailedRegenerationPreservesSnapshot(t *testing.T) {
	s, path, info := atlasFixture(t, 30)
	ctx := context.Background()
	o := DefaultAtlasOptions()
	o.MaxHosts = 10
	_, e := s.GenerateAtlas(ctx, path, "second", o, func(string) {})
	require.Error(t, e)
	require.Contains(t, e.Error(), "host limit exceeded")
	current, e := ReadAtlasInfo(ctx, path)
	require.NoError(t, e)
	require.Equal(t, info.ID, current.ID)
	c, cancel := context.WithCancel(ctx)
	cancel()
	_, e = s.GenerateAtlas(c, path, "third", DefaultAtlasOptions(), func(string) {})
	require.Error(t, e)
	current, e = ReadAtlasInfo(ctx, path)
	require.NoError(t, e)
	require.Equal(t, info.ID, current.ID)
}
func TestAtlasValidation(t *testing.T) {
	o := DefaultAtlasOptions()
	require.NoError(t, o.Validate())
	o.MaxHosts = 2000001
	require.Error(t, o.Validate())
	_, e := AtlasView(context.Background(), "unused", AtlasViewport{NodeLimit: 5001, EdgeLimit: 100})
	require.Error(t, e)
	_, e = AtlasInspect(context.Background(), "unused", "", 1, "invalid", 0)
	require.Error(t, e)
}

func TestAtlasDenseEdgeBudgetConservesEveryLink(t *testing.T) {
	s, path, _ := atlasFixture(t, 65)
	ctx := context.Background()
	tx, e := s.db.Begin()
	require.NoError(t, e)
	for a := 1; a <= 65; a++ {
		for b := 1; b <= 65; b++ {
			if a != b {
				_, e = tx.Exec("INSERT OR IGNORE INTO edges VALUES(?,?)", a, b)
				require.NoError(t, e)
			}
		}
	}
	require.NoError(t, tx.Commit())
	info, e := s.GenerateAtlas(ctx, path, "dense", DefaultAtlasOptions(), func(string) {})
	require.NoError(t, e)
	f, e := AtlasView(ctx, path, AtlasViewport{Snapshot: info.ID, Bounds: []float64{-1, -1, 2, 2}, NodeLimit: 500, EdgeLimit: 100})
	require.NoError(t, e)
	require.True(t, f.Rasterized)
	require.LessOrEqual(t, len(f.Edges), 100)
	require.Equal(t, 65*64, f.Links)
}

func TestAtlasFilterSwitchesAndPagination(t *testing.T) {
	s, path, _ := atlasFixture(t, 240)
	ctx := context.Background()
	o := DefaultAtlasOptions()
	o.VisitedOnly = false
	o.RemoveOrphans = false
	info, e := s.GenerateAtlas(ctx, path, "all", o, func(string) {})
	require.NoError(t, e)
	require.Equal(t, 242, info.Hosts)
	require.Equal(t, 240, info.Edges)
	o.VisitedOnly = true
	o.RemoveOrphans = false
	info, e = s.GenerateAtlas(ctx, path, "visited", o, func(string) {})
	require.NoError(t, e)
	require.Equal(t, 241, info.Hosts)
	require.Equal(t, 239, info.Edges)
	tx, e := s.db.Begin()
	require.NoError(t, e)
	for i := 2; i <= 240; i++ {
		_, e = tx.Exec("INSERT OR IGNORE INTO edges VALUES(1,?)", i)
		require.NoError(t, e)
	}
	require.NoError(t, tx.Commit())
	_, e = s.GenerateAtlas(ctx, path, "pages", DefaultAtlasOptions(), func(string) {})
	require.NoError(t, e)
	first, e := AtlasInspect(ctx, path, "pages", 1, "outgoing", 0)
	require.NoError(t, e)
	require.Len(t, first.Neighbors, 200)
	require.True(t, first.More)
	second, e := AtlasInspect(ctx, path, "pages", 1, "outgoing", 200)
	require.NoError(t, e)
	require.Len(t, second.Neighbors, 39)
	require.False(t, second.More)
	require.NotEqual(t, first.Neighbors[199].ID, second.Neighbors[0].ID)
}

func TestAtlasFocusPinsHostWithinBothRenderBudgets(t *testing.T) {
	s, path, _ := atlasFixture(t, 240)
	ctx := context.Background()
	tx, e := s.db.Begin()
	require.NoError(t, e)
	for i := 2; i <= 240; i++ {
		_, e = tx.Exec("INSERT OR IGNORE INTO edges VALUES(1,?)", i)
		require.NoError(t, e)
	}
	require.NoError(t, tx.Commit())
	info, e := s.GenerateAtlas(ctx, path, "focus", DefaultAtlasOptions(), func(string) {})
	require.NoError(t, e)
	frame, e := AtlasView(ctx, path, AtlasViewport{Snapshot: info.ID, Bounds: []float64{-1, -1, 2, 2}, NodeLimit: 50, EdgeLimit: 100, SelectedHost: 1})
	require.NoError(t, e)
	require.True(t, frame.Rasterized)
	require.LessOrEqual(t, len(frame.Nodes), 50)
	require.LessOrEqual(t, len(frame.Edges)+len(frame.Focus), 100)
	require.True(t, frame.Rasterized)
	found := false
	members := 0
	for _, n := range frame.Nodes {
		members += n.Members
		if n.HostID == 1 {
			found = true
		}
	}
	require.True(t, found)
	require.LessOrEqual(t, members, 50)
	require.Equal(t, 240, frame.Hosts)
	require.Equal(t, info.Edges, frame.Links)
}

func TestAtlasPendingBackfillDoesNotPublishEmptyHistory(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "legacy.sqlite"), true)
	require.NoError(t, e)
	defer s.Close()
	path := filepath.Join(t.TempDir(), "atlas.sqlite")
	_, e = s.GenerateAtlas(ctx, path, "pending", DefaultAtlasOptions(), func(string) {})
	require.Error(t, e)
	require.Contains(t, e.Error(), "backfill pending")
}

// Readers must see one immutable artifact while another client regenerates it.
func TestAtlasConcurrentSnapshotReplacement(t *testing.T) {
	s, path, first := atlasFixture(t, 60)
	ctx := context.Background()
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	_, err = s.db.Exec("UPDATE hosts SET name='replacement-' || name")
	require.NoError(t, err)
	_, err = s.GenerateAtlas(ctx, path, "replacement", DefaultAtlasOptions(), func(string) {})
	require.NoError(t, err)
	replacement, err := os.ReadFile(path)
	require.NoError(t, err)
	finished := make(chan error, 1)
	go func() {
		for i := 0; i < 100; i++ {
			data := original
			if i%2 == 1 {
				data = replacement
			}
			temp := path + ".replace"
			if err := os.WriteFile(temp, data, 0600); err != nil {
				finished <- err
				return
			}
			if err := os.Rename(temp, path); err != nil {
				finished <- err
				return
			}
		}
		finished <- nil
	}()
	for i := 0; i < 100; i++ {
		host, err := AtlasInspect(ctx, path, first.ID, 1, "outgoing", 0)
		if err != nil {
			require.Contains(t, err.Error(), "snapshot changed")
		} else {
			require.Equal(t, "h1.test", host.Host.Name)
			require.Equal(t, "h2.test", host.Neighbors[0].Name)
		}
		frame, err := AtlasView(ctx, path, AtlasViewport{Snapshot: first.ID, Bounds: []float64{-1, -1, 2, 2}, NodeLimit: 100, EdgeLimit: 200})
		if err != nil {
			require.Contains(t, err.Error(), "snapshot changed")
		} else {
			require.Equal(t, first.ID, frame.Snapshot)
			require.Len(t, frame.Nodes, first.Hosts)
		}
	}
	require.NoError(t, <-finished)
}

func TestAtlasCanOmitTrapHostsWithoutReplacingFullSnapshot(t *testing.T) {
	s, fullPath, full := atlasFixture(t, 20)
	ctx := context.Background()
	filteredPath := fullPath + ".traps"
	filtered, err := s.GenerateFilteredAtlas(ctx, filteredPath, "filtered", full.Options, []string{"H10.test."}, func(string) {})
	require.NoError(t, err)
	require.Equal(t, 1, filtered.HiddenHosts)
	require.Equal(t, 19, filtered.Hosts)
	require.Equal(t, 17, filtered.Edges)
	hidden, err := AtlasSearch(ctx, filteredPath, "h10.test", filtered.ID)
	require.NoError(t, err)
	require.Empty(t, hidden)
	original, err := AtlasSearch(ctx, fullPath, "h10.test", full.ID)
	require.NoError(t, err)
	require.Len(t, original, 1)
}
