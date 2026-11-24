package report

import (
	"context"
	"fmt"
	"github.com/iakinsey/delver/repetition"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAtlasRepeatedFoldingScale(t *testing.T) {
	if os.Getenv("DELVER_ATLAS_MILLION") != "1" {
		t.Skip("run make test-network-million")
	}
	const count = 100000
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "source.sqlite"), false)
	require.NoError(t, err)
	defer s.Close()
	tx, err := s.db.Begin()
	require.NoError(t, err)
	for i := 1; i <= count; i++ {
		name := fmt.Sprintf("fold-%06d.test", i)
		_, err = tx.Exec(`INSERT INTO hosts(id,name,known,visited) VALUES(?,?,1,1)`, i, name)
		require.NoError(t, err)
		if i > 1 {
			_, err = tx.Exec(`INSERT INTO edges VALUES(?,?)`, i-1, i)
			require.NoError(t, err)
		}
	}
	require.NoError(t, tx.Commit())
	for start := 1; start <= count; start += 128 {
		updates := []repetition.Update{}
		for i := start; i <= count && i < start+128; i++ {
			name := fmt.Sprintf("fold-%06d.test", i)
			family := fmt.Sprintf("%064x", (i-1)/10+1)
			updates = append(updates, repetition.Update{Revision: int64(i), Epoch: "scale", Host: &repetition.Host{URI: "http://" + name + "/", Site: name, Family: family, Epoch: "scale", Samples: 1, Eligible: 1, Dominance: 1}, Families: []repetition.Family{{ID: family, Epoch: "scale", Confirmed: true, Sites: 10, SupportSites: 10, Representative: "http://" + name + "/"}}})
		}
		require.NoError(t, s.RepeatedPages(updates))
	}
	o := DefaultAtlasOptions()
	o.FoldRepeatedPages = true
	path := filepath.Join(t.TempDir(), "atlas.sqlite")
	info, err := s.GenerateAtlas(ctx, path, "fold-scale", o, func(p string) { t.Log(p) })
	require.NoError(t, err)
	require.Equal(t, count, info.RawHosts)
	require.Equal(t, count/10, info.Hosts)
	matches, err := AtlasSearch(ctx, path, "fold-050005", info.ID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	members, err := AtlasMembers(ctx, path, info.ID, matches[0].ID, "", 0)
	require.NoError(t, err)
	require.Len(t, members.Members, 10)
	require.Equal(t, 9, members.Internal)
	frame, err := AtlasView(ctx, path, AtlasViewport{Snapshot: info.ID, Bounds: []float64{-1, -1, 2, 2}, NodeLimit: 50, EdgeLimit: 100})
	require.NoError(t, err)
	require.LessOrEqual(t, len(frame.Nodes), 50)
	require.LessOrEqual(t, len(frame.Edges), 100)
	runtime.GC()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	require.Less(t, memory.HeapAlloc, uint64(512<<20))
	t.Log("post-GC heap", memory.HeapAlloc)
}
