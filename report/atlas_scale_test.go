package report

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// Explicit heavy gate: make test-network-million. All records start in empty
// synthetic storage; every host participates in directed links.
func TestAtlasMillionHosts(t *testing.T) {
	if os.Getenv("DELVER_ATLAS_MILLION") != "1" {
		t.Skip("run make test-network-million")
	}
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "source.sqlite"), false)
	require.NoError(t, e)
	defer s.Close()
	var peak atomic.Uint64
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				for old := peak.Load(); m.HeapAlloc > old; old = peak.Load() {
					if peak.CompareAndSwap(old, m.HeapAlloc) {
						break
					}
				}
			}
		}
	}()
	defer func() { close(stop); <-stopped; t.Log("peak heap MiB", peak.Load()/1024/1024) }()
	start := time.Now()
	tx, e := s.db.Begin()
	require.NoError(t, e)
	ns, e := tx.Prepare("INSERT INTO hosts(id,name,known,visited) VALUES(?,?,1,1)")
	require.NoError(t, e)
	es, e := tx.Prepare("INSERT INTO edges VALUES(?,?)")
	require.NoError(t, e)
	const count = 1000000
	for i := 1; i <= count; i++ {
		_, e = ns.Exec(i, fmt.Sprintf("h%d.test", i))
		require.NoError(t, e)
		if i > 1 {
			_, e = es.Exec(i-1, i)
			require.NoError(t, e)
		}
		if i > 1 && i%1000 == 0 {
			_, e = es.Exec(1, i)
			require.NoError(t, e)
		}
		if i > 10 {
			_, e = es.Exec(i, i-10)
			require.NoError(t, e)
		}
	}
	ns.Close()
	es.Close()
	require.NoError(t, tx.Commit())
	t.Log("fixture", time.Since(start))
	runtime.GC()
	o := DefaultAtlasOptions()
	o.MaxHosts = count
	o.MaxEdges = count * 3
	o.MemoryMB = 1536
	o.Passes = 4
	path := filepath.Join(t.TempDir(), "atlas.sqlite")
	start = time.Now()
	info, e := s.GenerateAtlas(ctx, path, "million", o, func(s string) { t.Log(s, time.Since(start)) })
	require.NoError(t, e)
	require.Equal(t, count, info.Hosts)
	t.Log("generation", time.Since(start))
	require.LessOrEqual(t, peak.Load(), uint64(o.MemoryMB)*1024*1024, "generation exceeded its configured Go heap budget")
	runtime.GC()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	t.Log("post-GC heap MiB", memory.HeapAlloc/1024/1024)
	start = time.Now()
	frame, e := AtlasView(ctx, path, AtlasViewport{Snapshot: info.ID, Bounds: []float64{-1, -1, 2, 2}, NodeLimit: 2000, EdgeLimit: 8000})
	require.NoError(t, e)
	require.Equal(t, count, frame.Hosts)
	require.Equal(t, info.Edges, frame.Links)
	require.LessOrEqual(t, len(frame.Nodes), 2000)
	require.LessOrEqual(t, len(frame.Edges), 8000)
	t.Log("overview", time.Since(start), "nodes", len(frame.Nodes), "edges", len(frame.Edges))
	host, e := AtlasInspect(ctx, path, info.ID, 500000, "incoming", 0)
	require.NoError(t, e)
	require.NotEmpty(t, host.Neighbors)
	zoom := []float64{host.Host.X - .0005, host.Host.Y - .0005, host.Host.X + .0005, host.Host.Y + .0005}
	start = time.Now()
	detail, e := AtlasView(ctx, path, AtlasViewport{Snapshot: info.ID, Bounds: zoom, NodeLimit: 2000, EdgeLimit: 8000})
	require.NoError(t, e)
	require.LessOrEqual(t, len(detail.Nodes), 2000)
	t.Log("detail", time.Since(start), "hosts", detail.Hosts)
}
