package report

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/community"
	"gonum.org/v1/gonum/graph/simple"
	"image"
	"image/color"
	"image/png"
	"math"
	"math/rand"
	randv2 "math/rand/v2"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAtlasZoomRetainsRealHostGeometry(t *testing.T) {
	_, path, info := atlasFixture(t, 140)
	ctx := context.Background()
	full, e := AtlasView(ctx, path, AtlasViewport{Snapshot: info.ID, Bounds: []float64{-1, -1, 2, 2}, NodeLimit: 500, EdgeLimit: 2000})
	require.NoError(t, e)
	overview, e := AtlasView(ctx, path, AtlasViewport{Snapshot: info.ID, Bounds: []float64{-1, -1, 2, 2}, NodeLimit: 50, EdgeLimit: 100})
	require.NoError(t, e)
	byID := map[int64]AtlasNode{}
	for _, n := range full.Nodes {
		byID[n.HostID] = n
	}
	for _, n := range overview.Nodes {
		require.Equal(t, byID[n.HostID], n)
		require.Equal(t, 1, n.Members)
	}
	require.True(t, overview.Rasterized)
	require.False(t, overview.Grouped)
	node := overview.Nodes[0]
	detail, e := AtlasView(ctx, path, AtlasViewport{Snapshot: info.ID, Bounds: []float64{node.X - .001, node.Y - .001, node.X + .001, node.Y + .001}, NodeLimit: 50, EdgeLimit: 100})
	require.NoError(t, e)
	for _, n := range detail.Nodes {
		require.Equal(t, byID[n.HostID], n)
	}
	picked, e := AtlasPick(ctx, path, info.ID, node.X, node.Y, 1e-7)
	require.NoError(t, e)
	require.NotNil(t, picked)
	require.Equal(t, node.HostID, picked.ID)
}
func pixelRGB(img image.Image, x, y int) color.RGBA {
	r, g, b, a := img.At(x, y).RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}
func TestAtlasRasterContinuityAndCrossingEdges(t *testing.T) {
	_, path, info := atlasFixture(t, 2)
	ctx := context.Background()
	db, e := openAtlas(path)
	require.NoError(t, e)
	db.Close()
	db, e = sql.Open("sqlite3", path)
	require.NoError(t, e)
	defer db.Close()
	_, e = db.Exec(`UPDATE nodes SET x=CASE id WHEN 1 THEN .1 ELSE .9 END,y=.4;DELETE FROM spatial;INSERT INTO spatial SELECT id,x,x,y,y FROM nodes;INSERT INTO edges VALUES(2,1);DROP TABLE edge_spatial;DROP TABLE tiles;DROP INDEX node_authority;`)
	require.NoError(t, e)
	info.Edges = 2
	raw, e := json.Marshal(info)
	require.NoError(t, e)
	_, e = db.Exec("UPDATE meta SET value=?", string(raw))
	require.NoError(t, e)
	require.NoError(t, buildAtlasRaster(ctx, db, info, func(string) {}))
	tile, e := AtlasTile(ctx, path, AtlasTileRequest{Snapshot: info.ID, Z: 2, X: 1, Y: 1})
	require.NoError(t, e)
	img, e := png.Decode(bytes.NewReader(tile.PNG))
	require.NoError(t, e)
	require.Equal(t, 512, img.Bounds().Dx())
	// Neither endpoint is inside this tile. The real connecting line still crosses it.
	require.Greater(t, pixelRGB(img, 10, 307).B, uint8(25))
	require.Greater(t, pixelRGB(img, 500, 307).B, uint8(25))
	next, e := AtlasTile(ctx, path, AtlasTileRequest{Snapshot: info.ID, Z: 2, X: 2, Y: 1})
	require.NoError(t, e)
	right, e := png.Decode(bytes.NewReader(next.PNG))
	require.NoError(t, e)
	require.Equal(t, pixelRGB(img, 511, 307), pixelRGB(right, 0, 307))
	frame, e := AtlasView(ctx, path, AtlasViewport{Snapshot: info.ID, Bounds: []float64{.25, .25, .5, .5}, NodeLimit: 50, EdgeLimit: 100})
	require.NoError(t, e)
	require.Zero(t, frame.Hosts)
	require.Equal(t, 2, frame.Links)
	highlighted, e := AtlasTile(ctx, path, AtlasTileRequest{Snapshot: info.ID, SelectedHost: 1})
	require.NoError(t, e)
	selected, e := png.Decode(bytes.NewReader(highlighted.PNG))
	require.NoError(t, e)
	blue, orange := 0, 0
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			p := pixelRGB(selected, x, y)
			if p == (color.RGBA{71, 184, 255, 255}) {
				blue++
			}
			if p == (color.RGBA{255, 173, 87, 255}) {
				orange++
			}
		}
	}
	require.Greater(t, blue, 10)
	require.Greater(t, orange, 10)
	for _, o := range []AtlasTileRequest{{Snapshot: info.ID, Z: 17}, {Snapshot: info.ID, Z: 1, X: 2}, {Snapshot: "stale"}} {
		_, e = AtlasTile(ctx, path, o)
		require.Error(t, e)
	}
	_, e = AtlasSearch(ctx, path, "h", "stale")
	require.Error(t, e)
	_, e = AtlasPick(ctx, path, info.ID, .5, .5, 1)
	require.Error(t, e)
	info.Format = 0
	raw, _ = json.Marshal(info)
	_, e = db.Exec("UPDATE meta SET value=?", string(raw))
	require.NoError(t, e)
	_, e = AtlasTile(ctx, path, AtlasTileRequest{Snapshot: info.ID})
	require.Error(t, e)
	require.Contains(t, e.Error(), "regenerate")
}
func TestAtlasModularityFindsConnectedCommunities(t *testing.T) {
	vertices := make([]atlasVertex, 40)
	edges := []atlasLink{}
	add := func(a, b int) {
		vertices[a].out = append(vertices[a].out, b)
		vertices[b].in = append(vertices[b].in, a)
		edges = append(edges, atlasLink{a, b})
	}
	for a := 0; a < 40; a++ {
		for b := 0; b < 40; b++ {
			if a != b && a/20 == b/20 {
				add(a, b)
			}
		}
	}
	add(19, 20)
	// The compact graph relies on sorted run adjacency, just like the ledger reader.
	for i := range vertices {
		sort.Ints(vertices[i].out)
		sort.Ints(vertices[i].in)
	}
	groups, e := clusterAtlas(context.Background(), vertices, 800)
	require.NoError(t, e)
	require.Len(t, groups, 2)
	for i := 0; i < 20; i++ {
		require.Equal(t, vertices[0].cluster, vertices[i].cluster)
		require.Equal(t, vertices[20].cluster, vertices[i+20].cluster)
	}
	require.NotEqual(t, vertices[0].cluster, vertices[20].cluster)
	require.NoError(t, layoutAtlas(context.Background(), vertices, edges, groups, 12, rand.New(rand.NewSource(42))))
	var first, second [2]float64
	for i, v := range vertices {
		if i < 20 {
			first[0] += v.x / 20
			first[1] += v.y / 20
		} else {
			second[0] += v.x / 20
			second[1] += v.y / 20
		}
	}
	gap := math.Hypot(first[0]-second[0], first[1]-second[1])
	require.Greater(t, gap, 1.)
}

func TestAtlasCachedModularityMatchesReference(t *testing.T) {
	vertices := make([]atlasVertex, 40)
	reference := simple.NewWeightedUndirectedGraph(0, 0)
	for i := range vertices {
		reference.AddNode(simple.Node(i))
	}
	for a := range vertices {
		for b := range vertices {
			if a != b && a/20 == b/20 {
				vertices[a].out = append(vertices[a].out, b)
				vertices[b].in = append(vertices[b].in, a)
				if a < b {
					reference.SetWeightedEdge(simple.WeightedEdge{F: simple.Node(a), T: simple.Node(b), W: 2})
				}
			}
		}
	}
	vertices[19].out = append(vertices[19].out, 20)
	vertices[20].in = append(vertices[20].in, 19)
	reference.SetWeightedEdge(simple.WeightedEdge{F: simple.Node(19), T: simple.Node(20), W: 1})
	for i := range vertices {
		sort.Ints(vertices[i].out)
		sort.Ints(vertices[i].in)
	}
	g, e := atlasModInput(context.Background(), vertices)
	require.NoError(t, e)
	labels, e := atlasModularize(context.Background(), g, 1, 12)
	require.NoError(t, e)
	groups := make([][]graph.Node, 2)
	for i, l := range labels {
		require.Less(t, l, 2)
		groups[l] = append(groups[l], simple.Node(i))
	}
	expected := community.Modularize(reference, 1, randv2.NewPCG(42, 1)).Communities()
	require.InDelta(t, community.Q(reference, expected, 1), community.Q(reference, groups, 1), 1e-12)
	require.Greater(t, community.Q(reference, groups, 1), .49)
}
func TestAtlasModularityAggregationPreservesWeights(t *testing.T) {
	nodes := []atlasVertex{{out: []int{1}, in: []int{1}}, {out: []int{0, 2}, in: []int{0}}, {in: []int{1}}}
	g, e := atlasModInput(context.Background(), nodes)
	require.NoError(t, e)
	next, e := atlasModReduce(context.Background(), g, []int{0, 0, 1}, 2)
	require.NoError(t, e)
	require.Equal(t, 6., g.total)
	require.Equal(t, g.total, next.total)
	require.Equal(t, []float64{5, 1}, next.degree)
	require.Equal(t, []float64{4, 0}, next.self)
	require.Equal(t, []int{1, 0}, next.peer)
	require.Equal(t, []float64{1, 1}, next.weight)
	c, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = atlasModularize(c, g, 1, 12)
	require.ErrorIs(t, e, context.Canceled)
}

func TestPurgedAtlasCannotBeRepopulatedByAnEarlierTileRender(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.atlas.sqlite")
	key := path + ":old-snapshot:0:0:0"
	old := AtlasEpoch(path)
	cacheAtlasTile(key, []byte("old tile"), path, old)
	require.NotEmpty(t, cachedAtlasTile(key))
	ForgetAtlas(path)
	require.Greater(t, AtlasEpoch(path), old)
	require.Empty(t, cachedAtlasTile(key))
	// A render already in progress still carries its original epoch.
	cacheAtlasTile(key, []byte("late tile"), path, old)
	require.Empty(t, cachedAtlasTile(key))
	other := filepath.Join(t.TempDir(), "other.atlas.sqlite")
	otherKey := other + ":current:0:0:0"
	cacheAtlasTile(otherKey, []byte("unrelated tile"), other, AtlasEpoch(other))
	ForgetAtlas(path)
	require.NotEmpty(t, cachedAtlasTile(otherKey))
	ForgetAtlas(other)
}
