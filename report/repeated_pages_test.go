package report

import (
	"context"
	"fmt"
	"github.com/iakinsey/delver/repetition"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestRepeatedProjectionFoldingAndSnapshotMembership(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "report.sqlite"), false)
	require.NoError(t, e)
	defer s.Close()
	names := []string{"mirror-one.test", "mirror-two.test", "mirror-three.test", "independent.test"}
	urls := []URL{}
	for _, name := range names {
		for _, dest := range names {
			if name != dest {
				urls = append(urls, URL{URI: "http://" + dest + "/", Origin: "http://" + name + "/"})
			}
		}
	}
	require.NoError(t, s.Discover(ctx, urls))
	for _, name := range names {
		require.NoError(t, s.Response(ctx, "http://"+name+"/", 200))
	}
	family := repetition.Family{ID: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Epoch: "epoch", Confirmed: true, Sites: 3, SupportSites: 3, Representative: "http://mirror-one.test/"}
	updates := []repetition.Update{}
	for i, name := range names[:3] {
		updates = append(updates, repetition.Update{Revision: int64(i + 1), Epoch: "epoch", Families: []repetition.Family{family}, Host: &repetition.Host{URI: "http://" + name + "/", Site: name, Family: family.ID, Epoch: "epoch", Samples: 1, Dominance: 1}})
	}
	require.NoError(t, s.RepeatedPages(updates))
	require.NoError(t, s.RepeatedPages(updates))
	path := filepath.Join(t.TempDir(), "fold.sqlite")
	o := DefaultAtlasOptions()
	o.FoldRepeatedPages = true
	info, e := s.GenerateAtlas(ctx, path, "folded", o, func(string) {})
	require.NoError(t, e)
	require.Equal(t, 4, info.RawHosts)
	require.Equal(t, 12, info.RawEdges)
	require.Equal(t, 2, info.Hosts)
	require.Equal(t, 2, info.Edges)
	matches, e := AtlasSearch(ctx, path, "mirror-two", "folded")
	require.NoError(t, e)
	require.Len(t, matches, 1)
	id := matches[0].ID
	page, e := AtlasMembers(ctx, path, "folded", id, "", 0)
	require.NoError(t, e)
	require.Len(t, page.Members, 3)
	require.Equal(t, 6, page.Internal)
	page, e = AtlasMembers(ctx, path, "folded", id, "two", 0)
	require.NoError(t, e)
	require.Len(t, page.Members, 1)
	require.Equal(t, "mirror-two.test", page.Members[0].Name)
	frame, e := AtlasView(ctx, path, AtlasViewport{Snapshot: "folded", Bounds: []float64{0, 0, 1, 1}, NodeLimit: 50, EdgeLimit: 100})
	require.NoError(t, e)
	require.Len(t, frame.Nodes, 2)
	for _, edge := range frame.Edges {
		require.Equal(t, 3, edge.Links)
	}
	var familyNode AtlasNode
	for _, node := range frame.Nodes {
		if node.HostID == id {
			familyNode = node
		}
	}
	require.Equal(t, 3, familyNode.Members)
	require.Equal(t, 6, familyNode.Internal)
	// Exemption affects future generations; immutable coordinates and membership
	// remain pinned to this artifact while live policy changes independently.
	family.Exempt = true
	require.NoError(t, s.RepeatedPages([]repetition.Update{{Revision: 4, Epoch: "epoch", Families: []repetition.Family{family}}}))
	page, e = AtlasMembers(ctx, path, "folded", id, "", 0)
	require.NoError(t, e)
	require.Len(t, page.Members, 3)
	closeup, e := AtlasView(ctx, path, AtlasViewport{Snapshot: "folded", Bounds: []float64{familyNode.X - .001, familyNode.Y - .001, familyNode.X + .001, familyNode.Y + .001}, NodeLimit: 50, EdgeLimit: 100})
	require.NoError(t, e)
	require.Len(t, closeup.Nodes, 1)
	require.Equal(t, familyNode, closeup.Nodes[0])
	info, e = s.GenerateAtlas(ctx, path, "exempt", o, func(string) {})
	require.NoError(t, e)
	require.Equal(t, 4, info.Hosts)
	_, e = AtlasMembers(ctx, path, "folded", id, "", 0)
	require.Error(t, e)
	require.Contains(t, e.Error(), "snapshot changed")
	counts, e := s.Counts(ctx)
	require.NoError(t, e)
	require.EqualValues(t, 4, counts.Hosts)
	require.EqualValues(t, 12, counts.Edges)
}
func TestRepeatedProjectionEpochAndProtectionPurge(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "report.sqlite"), false)
	require.NoError(t, e)
	defer s.Close()
	f := repetition.Family{ID: "family", Epoch: "old", Confirmed: true, Representative: "http://member.test/"}
	require.NoError(t, s.RepeatedPages([]repetition.Update{{Revision: 1, Epoch: "old", Families: []repetition.Family{f}, Host: &repetition.Host{Site: "member.test", URI: "http://member.test/", Family: f.ID, Epoch: "old"}}}))
	require.NoError(t, s.PurgeSite(ctx, "member.test"))
	var n int
	require.NoError(t, s.db.QueryRow(`SELECT count(*) FROM repeated_sites`).Scan(&n))
	require.Zero(t, n)
	require.NoError(t, s.db.QueryRow(`SELECT count(*) FROM repeated_families`).Scan(&n))
	require.Zero(t, n)
	require.NoError(t, s.RepeatedPages([]repetition.Update{{Revision: 2, Epoch: "new"}}))
	var epoch string
	require.NoError(t, s.db.QueryRow(`SELECT epoch FROM repeated_meta WHERE id=1`).Scan(&epoch))
	require.Equal(t, "new", epoch)
}

func TestRepeatedFoldingDoesNotChangeRawIncomingPropagation(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "report.sqlite"), false)
	require.NoError(t, err)
	defer s.Close()
	urls := []URL{{URI: "http://a.test/", Origin: "http://outside.test/"}, {URI: "http://outside.test/", Origin: "http://a.test/"}, {URI: "http://b.test/", Origin: "http://c.test/"}, {URI: "http://c.test/", Origin: "http://b.test/"}}
	require.NoError(t, s.Discover(ctx, urls))
	for _, name := range []string{"a", "b", "c", "outside"} {
		require.NoError(t, s.Response(ctx, "http://"+name+".test/", 200))
	}
	f := repetition.Family{ID: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Epoch: "e", Confirmed: true, Sites: 3, SupportSites: 3, Representative: "http://a.test/"}
	for i, name := range []string{"a", "b", "c"} {
		require.NoError(t, s.RepeatedPages([]repetition.Update{{Revision: int64(i + 1), Epoch: "e", Families: []repetition.Family{f}, Host: &repetition.Host{Site: name + ".test", URI: "http://" + name + ".test/", Family: f.ID, Epoch: "e", Samples: 1, Eligible: 1, Dominance: 1}}}))
	}
	o := DefaultAtlasOptions()
	o.FoldRepeatedPages = true
	path := filepath.Join(t.TempDir(), "fold.sqlite")
	_, err = s.GenerateAtlas(ctx, path, "raw-propagation", o, func(string) {})
	require.NoError(t, err)
	sites, _, _, err := s.IncomingSites(ctx, "a.test", 1)
	require.NoError(t, err)
	require.Equal(t, []SiteDistance{{"a.test", 0}, {"outside.test", 1}}, sites)
	require.NoError(t, s.PurgeSite(ctx, "a.test"))
	counts, err := s.Counts(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 3, counts.Hosts)
	require.EqualValues(t, 2, counts.Edges)
}
func TestRepeatedFoldOptionWithoutFamiliesRetainsHostSearch(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "report.sqlite"), false)
	require.NoError(t, err)
	defer s.Close()
	require.NoError(t, s.Discover(ctx, []URL{{URI: "http://a.test/", Origin: "http://b.test/"}}))
	require.NoError(t, s.Response(ctx, "http://a.test/", 200))
	require.NoError(t, s.Response(ctx, "http://b.test/", 200))
	o := DefaultAtlasOptions()
	o.FoldRepeatedPages = true
	path := filepath.Join(t.TempDir(), "fold.sqlite")
	info, err := s.GenerateAtlas(ctx, path, "no-families", o, func(string) {})
	require.NoError(t, err)
	require.Equal(t, 2, info.Hosts)
	found, err := AtlasSearch(ctx, path, "a.test", info.ID)
	require.NoError(t, err)
	require.Len(t, found, 1)
	page, err := AtlasMembers(ctx, path, info.ID, found[0].ID, "", 0)
	require.NoError(t, err)
	require.Empty(t, page.Members)
}

func TestRepeatedFoldedMemberPaginationAndSearch(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "report.sqlite"), false)
	require.NoError(t, err)
	defer s.Close()
	f := repetition.Family{ID: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Epoch: "e", Confirmed: true, Sites: 125, SupportSites: 100, Representative: "http://member-000.test/"}
	for i := 0; i < 125; i++ {
		uri := fmt.Sprintf("http://member-%03d.test/", i)
		origin := fmt.Sprintf("http://member-%03d.test/", (i+1)%125)
		require.NoError(t, s.Discover(ctx, []URL{{URI: uri, Origin: origin}}))
		require.NoError(t, s.Response(ctx, uri, 200))
		require.NoError(t, s.RepeatedPages([]repetition.Update{{Revision: int64(i + 1), Epoch: "e", Families: []repetition.Family{f}, Host: &repetition.Host{Site: fmt.Sprintf("member-%03d.test", i), URI: uri, Family: f.ID, Epoch: "e", Samples: 1, Eligible: 1, Dominance: 1}}}))
	}
	path := filepath.Join(t.TempDir(), "fold.sqlite")
	o := DefaultAtlasOptions()
	o.FoldRepeatedPages = true
	info, err := s.GenerateAtlas(ctx, path, "members", o, func(string) {})
	require.NoError(t, err)
	require.Equal(t, 1, info.Hosts)
	matches, err := AtlasSearch(ctx, path, "member-124.test", info.ID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	first, err := AtlasMembers(ctx, path, info.ID, matches[0].ID, "", 0)
	require.NoError(t, err)
	require.Len(t, first.Members, 100)
	require.True(t, first.More)
	last, err := AtlasMembers(ctx, path, info.ID, matches[0].ID, "", 100)
	require.NoError(t, err)
	require.Len(t, last.Members, 25)
	require.False(t, last.More)
	require.Equal(t, "member-124.test", last.Members[24].Name)
	filtered, err := AtlasMembers(ctx, path, info.ID, matches[0].ID, "124", 0)
	require.NoError(t, err)
	require.Len(t, filtered.Members, 1)
}
