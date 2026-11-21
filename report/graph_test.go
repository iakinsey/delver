package report

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
	"time"
)

func TestGraphDepthImportanceAndDirectedLinks(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "r.sqlite"), false)
	require.NoError(t, e)
	defer s.Close()
	require.NoError(t, s.Discover(ctx, []URL{{URI: "https://a.test/"}, {URI: "https://b.test/", Origin: "https://a.test/"}, {URI: "https://c.test/", Origin: "https://b.test/"}, {URI: "https://a.test/", Origin: "https://c.test/"}, {URI: "https://a.test/other", Origin: "https://a.test/"}, {URI: "https://leaf.test/", Origin: "https://a.test/"}, {URI: "https://b.test/", Origin: "https://leaf.test/"}}))
	zero, e := s.Graph(ctx, GraphOptions{Host: "a.test", Depth: 0})
	require.NoError(t, e)
	require.Len(t, zero.Nodes, 1)
	require.Empty(t, zero.Edges)
	full, e := s.Graph(ctx, GraphOptions{Host: "a.test", Depth: 2})
	require.NoError(t, e)
	require.Len(t, full.Nodes, 4)
	require.Len(t, full.Edges, 5)
	require.False(t, full.Grouped)
	require.False(t, full.Sampled)
	require.Equal(t, zero.Nodes[0].Authority, full.Nodes[0].Authority)
	require.NoError(t, s.Response(ctx, "https://a.test/", 404))
	updated, e := s.Graph(ctx, GraphOptions{Host: "a.test", Depth: 2})
	require.NoError(t, e)
	require.Equal(t, full.Revision, updated.Revision)
	require.EqualValues(t, 1, updated.Nodes[0].Visited)
	_, e = s.Graph(ctx, GraphOptions{Host: "missing.test", Depth: 2})
	require.Error(t, e)
	_, e = s.Graph(ctx, GraphOptions{Host: "a.test", Depth: -1})
	require.Error(t, e)
}
func TestLargeGraphPreservesDeepBranchesAndGroupedMembership(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "r.sqlite"), false)
	require.NoError(t, e)
	defer s.Close()
	records := []URL{{URI: "https://root.test/"}}
	for i := 0; i < 200; i++ {
		records = append(records, URL{URI: fmt.Sprintf("https://leaf%03d.test/", i), Origin: "https://root.test/"})
	}
	parent := "https://root.test/"
	for i := 0; i < 8; i++ {
		uri := fmt.Sprintf("https://deep%02d.test/", i)
		records = append(records, URL{URI: uri, Origin: parent})
		parent = uri
	}
	require.NoError(t, s.Discover(ctx, records))
	hosts, e := s.Graph(ctx, GraphOptions{Host: "root.test", Depth: 8, NodeLimit: 30, EdgeLimit: 64})
	require.NoError(t, e)
	require.True(t, hosts.Sampled)
	require.LessOrEqual(t, len(hosts.Nodes), 30)
	deep := false
	for _, n := range hosts.Nodes {
		if n.Depth == 8 {
			deep = true
		}
	}
	require.True(t, deep, "dense first hop hid the deepest branch")
	grouped, e := s.Graph(ctx, GraphOptions{Host: "root.test", Depth: 8, Mode: "grouped", NodeLimit: 30, EdgeLimit: 64})
	require.NoError(t, e)
	require.True(t, grouped.Grouped)
	require.LessOrEqual(t, len(grouped.Nodes), 30)
	require.LessOrEqual(t, len(grouped.Edges), 64)
	members, links := 0, 0
	for _, n := range grouped.Nodes {
		members += n.Members
		links += n.InternalLinks
	}
	for _, edge := range grouped.Edges {
		links += edge.Links
	}
	require.Equal(t, grouped.TotalHosts, members)
	require.Equal(t, grouped.TotalEdges, links)
	var group GraphNode
	for _, n := range grouped.Nodes {
		if n.Group && n.Members > 1 {
			group = n
			break
		}
	}
	require.NotEmpty(t, group.ID)
	expanded, e := s.Graph(ctx, GraphOptions{Host: "root.test", Depth: 8, Mode: "grouped", NodeLimit: 30, EdgeLimit: 64, Expanded: []string{group.ID}})
	require.NoError(t, e)
	still := false
	for _, n := range expanded.Nodes {
		still = still || n.ID == group.ID
	}
	require.False(t, still, "expansion was silently undone")
	search, e := s.Search(ctx, "leaf199", "", 20)
	require.NoError(t, e)
	require.Len(t, search, 1)
}
func TestBridgeRolesAndAuthorityAcrossCommunities(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "r.sqlite"), false)
	require.NoError(t, e)
	defer s.Close()
	ctx := context.Background()
	rs := []URL{}
	for _, side := range []string{"a", "b"} {
		for i := 0; i < 10; i++ {
			for j := 0; j < 10; j++ {
				if i != j {
					rs = append(rs, URL{URI: fmt.Sprintf("https://%s%d.test/", side, j), Origin: fmt.Sprintf("https://%s%d.test/", side, i)})
				}
			}
		}
	}
	rs = append(rs, URL{URI: "https://bridge.test/", Origin: "https://a0.test/"}, URL{URI: "https://b0.test/", Origin: "https://bridge.test/"})
	require.NoError(t, s.Discover(ctx, rs))
	g, e := s.Graph(ctx, GraphOptions{Host: "bridge.test", Depth: 3})
	require.NoError(t, e)
	marked := false
	for _, n := range g.Nodes {
		if n.Bridges > 0 {
			marked = true
		}
		require.Greater(t, n.Authority, 0.0)
	}
	require.True(t, marked)
}

func TestGraphCancellationCacheDeletionAndDeepPathBudget(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "r.sqlite")
	s, e := Open(path, false)
	require.NoError(t, e)
	defer s.Close()
	records := []URL{{URI: "https://h0.test/"}}
	for i := 1; i < 30; i++ {
		records = append(records, URL{URI: fmt.Sprintf("https://h%d.test/", i), Origin: fmt.Sprintf("https://h%d.test/", i-1)})
	}
	require.NoError(t, s.Discover(ctx, records))
	_, e = s.Graph(ctx, GraphOptions{Host: "h0.test", Depth: 29, NodeLimit: 10})
	require.Error(t, e)
	require.Contains(t, e.Error(), "path to depth")
	v, e := s.Graph(ctx, GraphOptions{Host: "h0.test", Depth: 29, NodeLimit: 10, Mode: "grouped"})
	require.NoError(t, e)
	require.Equal(t, 30, v.TotalHosts)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, e = s.Graph(canceled, GraphOptions{Host: "h0.test", Depth: 29})
	require.ErrorIs(t, e, context.Canceled)
	Forget(path)
	analyses.gate <- struct{}{}
	_, cached := analyses.values[path]
	<-analyses.gate
	require.False(t, cached)
	// A later query can rebuild; cancellation/deletion must not leak the gate.
	_, e = s.Graph(ctx, GraphOptions{Host: "h0.test", Depth: 2})
	require.NoError(t, e)
}

func TestDenseGroupedProjectionConservesLinksUnderSmallBudget(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "dense.sqlite"), false)
	require.NoError(t, e)
	defer s.Close()
	ctx := context.Background()
	records := []URL{}
	for i := 0; i < 100; i++ {
		for j := 0; j < 100; j++ {
			if i != j {
				records = append(records, URL{URI: fmt.Sprintf("https://d%d.test/", j), Origin: fmt.Sprintf("https://d%d.test/", i)})
			}
		}
	}
	require.NoError(t, s.Discover(ctx, records))
	deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	v, e := s.Graph(deadline, GraphOptions{Host: "d0.test", Depth: 1, Mode: "grouped", NodeLimit: 20, EdgeLimit: 32})
	require.NoError(t, e)
	require.LessOrEqual(t, len(v.Nodes), 20)
	require.LessOrEqual(t, len(v.Edges), 32)
	members, links := 0, 0
	for _, n := range v.Nodes {
		members += n.Members
		links += n.InternalLinks
	}
	for _, edge := range v.Edges {
		links += edge.Links
	}
	require.Equal(t, 100, members)
	require.Equal(t, 9900, links)
}

func TestHostBackboneRemainsConnectedWhenLinkBudgetIsSmaller(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "limited.sqlite"), false)
	require.NoError(t, e)
	defer s.Close()
	records := []URL{}
	for i := 0; i < 100; i++ {
		records = append(records, URL{URI: fmt.Sprintf("https://leaf%d.test/", i), Origin: "https://root.test/"})
	}
	require.NoError(t, s.Discover(context.Background(), records))
	v, e := s.Graph(context.Background(), GraphOptions{Host: "root.test", Depth: 1, NodeLimit: 500, EdgeLimit: 32})
	require.NoError(t, e)
	require.Len(t, v.Nodes, 33)
	require.Len(t, v.Edges, 32)
	connected := map[string]bool{}
	for _, edge := range v.Edges {
		connected[edge.Source] = true
		connected[edge.Target] = true
	}
	for _, node := range v.Nodes {
		require.True(t, connected[node.ID], node.Name)
	}
}
