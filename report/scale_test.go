package report

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func scaleRoot(t *testing.T) string {
	t.Helper()
	base := os.Getenv("DELVER_REPORT_SCALE_ROOT")
	if base == "" {
		return t.TempDir()
	}
	require.NoError(t, os.MkdirAll(base, 0700))
	root, e := os.MkdirTemp(base, "empty-report-")
	require.NoError(t, e)
	t.Cleanup(func() { os.RemoveAll(root) })
	return root
}
func scaleEvidence(t *testing.T, name string, values map[string]interface{}) {
	t.Helper()
	if root := os.Getenv("DELVER_VALIDATION_ROOT"); root != "" {
		require.NoError(t, os.MkdirAll(root, 0700))
		b, e := json.MarshalIndent(values, "", "  ")
		require.NoError(t, e)
		require.NoError(t, os.WriteFile(filepath.Join(root, name+".json"), b, 0600))
	}
	b, _ := json.Marshal(values)
	t.Log(string(b))
}
func TestReportMillionURLs(t *testing.T) {
	if os.Getenv("DELVER_REPORT_MILLION") != "1" {
		t.Skip("set DELVER_REPORT_MILLION=1")
	}
	ctx := context.Background()
	path := filepath.Join(scaleRoot(t), "report.sqlite")
	s, e := Open(path, false)
	require.NoError(t, e)
	start := time.Now()
	for at := 0; at < 1000000; at += 2048 {
		end := at + 2048
		if end > 1000000 {
			end = 1000000
		}
		batch := make([]URL, 0, end-at)
		for i := at; i < end; i++ {
			batch = append(batch, URL{URI: fmt.Sprintf("https://h%04d.test/page/%d", i%1000, i), Origin: fmt.Sprintf("https://h%04d.test/root", (i+1)%1000)})
		}
		require.NoError(t, s.Discover(ctx, batch))
	}
	for i := 0; i < 1000; i++ {
		require.NoError(t, s.Response(ctx, fmt.Sprintf("https://h%04d.test/page/%d", i%1000, i), 200))
		require.NoError(t, s.Response(ctx, fmt.Sprintf("https://h%04d.test/page/%d", i%1000, i), 404))
	}
	counts, e := s.Counts(ctx)
	require.NoError(t, e)
	require.EqualValues(t, 1000000, counts.Known)
	require.EqualValues(t, 1000, counts.Visited)
	require.EqualValues(t, 999000, counts.NotVisited)
	require.EqualValues(t, 1000, counts.Hosts)
	require.EqualValues(t, 1000, counts.Edges)
	require.NoError(t, s.Close())
	s, e = Open(path, false)
	require.NoError(t, e)
	defer s.Close()
	restored, e := s.Counts(ctx)
	require.NoError(t, e)
	require.Equal(t, counts, restored)
	require.NoError(t, s.Discover(ctx, []URL{{URI: "https://h0000.test/page/0"}}))
	after, e := s.Counts(ctx)
	require.NoError(t, e)
	require.Equal(t, counts, after)
	runtime.GC()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	require.Less(t, memory.HeapAlloc, uint64(512<<20))
	info, e := os.Stat(path)
	require.NoError(t, e)
	scaleEvidence(t, "report-million", map[string]interface{}{"urls": counts.Known, "visited": counts.Visited, "elapsed_seconds": time.Since(start).Seconds(), "heap_bytes": memory.HeapAlloc, "database_bytes": info.Size()})
}
func TestReportHundredThousandHostGraph(t *testing.T) {
	if os.Getenv("DELVER_REPORT_GRAPH_SCALE") != "1" {
		t.Skip("set DELVER_REPORT_GRAPH_SCALE=1")
	}
	ctx := context.Background()
	s, e := Open(filepath.Join(scaleRoot(t), "report.sqlite"), false)
	require.NoError(t, e)
	defer s.Close()
	start := time.Now()
	batch := []URL{}
	add := func(a, b int) {
		batch = append(batch, URL{URI: fmt.Sprintf("https://h%06d.test/", b), Origin: fmt.Sprintf("https://h%06d.test/", a)})
		if len(batch) == 2048 {
			require.NoError(t, s.Discover(ctx, batch))
			batch = nil
		}
	}
	for i := 1; i < 100000; i++ {
		add(0, i)
		for j := 1; j <= 9; j++ {
			add(i, (i+j)%100000)
		}
	}
	for i := 1; i <= 10; i++ {
		add(i, i+20)
	}
	require.NoError(t, s.Discover(ctx, batch))
	counts, e := s.Counts(ctx)
	require.NoError(t, e)
	require.EqualValues(t, 100000, counts.Hosts)
	require.EqualValues(t, 1000000, counts.Edges)
	setup := time.Since(start)
	searchStart := time.Now()
	suggestions, e := s.Search(ctx, "", "", 20)
	require.NoError(t, e)
	require.Len(t, suggestions, 20)
	require.Equal(t, "h000000.test", suggestions[0].Name)
	require.EqualValues(t, 99999, suggestions[0].Connections)
	next, e := s.Search(ctx, "", suggestions[19].Cursor, 20)
	require.NoError(t, e)
	for _, row := range next {
		for _, old := range suggestions {
			require.NotEqual(t, old.ID, row.ID)
		}
	}
	searchTime := time.Since(searchStart)
	t.Logf("Ranked host search: two pages in %s", searchTime)
	require.Less(t, searchTime, 10*time.Second, "two ranked suggestion pages must remain bounded")
	start = time.Now()
	_, e = s.topology(ctx)
	require.NoError(t, e)
	analysisTime := time.Since(start)
	start = time.Now()
	v, e := s.Graph(ctx, GraphOptions{Host: "h000000.test", Depth: 2, Mode: "grouped"})
	require.NoError(t, e)
	require.Equal(t, 100000, v.TotalHosts)
	require.LessOrEqual(t, len(v.Nodes), 500)
	require.LessOrEqual(t, len(v.Edges), 2000)
	require.True(t, v.Grouped)
	members, links := 0, 0
	for _, n := range v.Nodes {
		members += n.Members
		links += n.InternalLinks
	}
	for _, edge := range v.Edges {
		links += edge.Links
	}
	require.Equal(t, 100000, members)
	require.Equal(t, 1000000, links)
	cold := time.Since(start)
	require.Less(t, analysisTime+cold, 30*time.Second, "cold graph must fit the API deadline")
	start = time.Now()
	v, e = s.Graph(ctx, GraphOptions{Host: "h000000.test", Depth: 2, Mode: "hosts"})
	require.NoError(t, e)
	require.LessOrEqual(t, len(v.Nodes), 500)
	require.LessOrEqual(t, len(v.Edges), 2000)
	require.True(t, v.Sampled)
	warm := time.Since(start)
	runtime.GC()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	require.Less(t, memory.HeapAlloc, uint64(512<<20))
	scaleEvidence(t, "report-host-graph", map[string]interface{}{"hosts": counts.Hosts, "edges": counts.Edges, "setup_seconds": setup.Seconds(), "two_search_pages_seconds": searchTime.Seconds(), "cold_seconds": cold.Seconds() + analysisTime.Seconds(), "analysis_seconds": analysisTime.Seconds(), "projection_seconds": cold.Seconds(), "warm_seconds": warm.Seconds(), "heap_bytes": memory.HeapAlloc, "rendered_nodes": len(v.Nodes), "rendered_edges": len(v.Edges)})
}
