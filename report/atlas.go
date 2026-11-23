package report

// Atlas is an immutable, disk-backed graph snapshot. Viewport responses are
// bounded hit-testing overlays; raster tiles retain every host and link.
import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"time"

	"github.com/iakinsey/delver/site"
)

type AtlasOptions struct {
	MaxHosts      int  `json:"max_hosts"`
	MaxEdges      int  `json:"max_edges"`
	MemoryMB      int  `json:"memory_mb"`
	Passes        int  `json:"passes"`
	ClusterSize   int  `json:"cluster_size"`
	VisitedOnly   bool `json:"visited_only"`
	RemoveOrphans bool `json:"remove_orphans"`
}

func DefaultAtlasOptions() AtlasOptions {
	return AtlasOptions{200000, 1000000, 512, 12, 800, true, true}
}
func (o AtlasOptions) Validate() error {
	if o.MaxHosts < 10 || o.MaxHosts > 2000000 || o.MaxEdges < 10 || o.MaxEdges > 10000000 || o.MemoryMB < 64 || o.MemoryMB > 4096 || o.Passes < 2 || o.Passes > 40 || o.ClusterSize < 20 || o.ClusterSize > 5000 {
		return fmt.Errorf("generation limits: hosts 10–2000000, edges 10–10000000, memory 64–4096 MiB, passes 2–40, cluster size 20–5000")
	}
	return nil
}

type AtlasInfo struct {
	HiddenHosts int          `json:"hidden_hosts,omitempty"`
	Format      int          `json:"format"`
	ID          string       `json:"id"`
	Created     time.Time    `json:"created"`
	Revision    int64        `json:"revision"`
	Partial     bool         `json:"partial"`
	Hosts       int          `json:"hosts"`
	Edges       int          `json:"edges"`
	Clusters    int          `json:"clusters"`
	Options     AtlasOptions `json:"options"`
}
type atlasVertex struct {
	id             int64
	name           string
	known, visited int64
	out, in        []int
	cluster        int
	rank, x, y     float64
}
type atlasLink struct{ a, b int }

// GenerateAtlas holds a consistent WAL read snapshot, never the crawler writer
// lock. Limits are checked while reading, before appending more records.
func (s *Store) GenerateAtlas(ctx context.Context, target, id string, o AtlasOptions, progress func(string)) (AtlasInfo, error) {
	return s.GenerateFilteredAtlas(ctx, target, id, o, nil, progress)
}

func (s *Store) GenerateFilteredAtlas(ctx context.Context, target, id string, o AtlasOptions, excludedSites []string, progress func(string)) (info AtlasInfo, err error) {
	excluded := make(map[string]bool, len(excludedSites))
	for _, name := range excludedSites {
		excluded[site.Key(name)] = true
	}
	if err = o.Validate(); err != nil {
		return
	}
	progress("Reading snapshot")
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return
	}
	defer tx.Rollback()
	info = AtlasInfo{Format: 2, ID: id, Created: time.Now().UTC(), Options: o}
	var partial, backfilled int
	if err = tx.QueryRowContext(ctx, "SELECT revision,partial,backfilled FROM meta WHERE id=1").Scan(&info.Revision, &partial, &backfilled); err != nil {
		return
	}
	info.Partial = partial != 0
	if info.Partial && backfilled == 0 {
		err = fmt.Errorf("report backfill pending; open Report before generating")
		return
	}
	nodes := []atlasVertex{}
	index := map[int64]int{}
	used := int64(32 * 1024 * 1024)
	budget := int64(o.MemoryMB) * 1024 * 1024
	check := func(extra int64) error {
		used += extra
		if used > budget {
			return fmt.Errorf("graph working-set estimate exceeds %d MiB; increase memory budget or restrict hosts", o.MemoryMB)
		}
		return ctx.Err()
	}
	query := "SELECT id,name,known,visited FROM hosts"
	if o.VisitedOnly {
		query += " WHERE visited>0"
	}
	query += " ORDER BY id"
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return
	}
	for rows.Next() {
		var v atlasVertex
		if err = rows.Scan(&v.id, &v.name, &v.known, &v.visited); err != nil {
			break
		}
		if len(excluded) > 0 && excluded[site.Key(v.name)] {
			info.HiddenHosts++
			continue
		}
		if len(v.name) > 253 {
			err = fmt.Errorf("host name exceeds 253 bytes")
			break
		}
		if len(nodes) >= o.MaxHosts {
			err = fmt.Errorf("host limit exceeded (%d); no partial snapshot published", o.MaxHosts)
			break
		}
		if err = check(int64(1024 + 2*len(v.name))); err != nil {
			break
		}
		index[v.id] = len(nodes)
		nodes = append(nodes, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return
	}
	edges := []atlasLink{}
	rows, err = tx.QueryContext(ctx, "SELECT source,target FROM edges ORDER BY source,target")
	if err != nil {
		return
	}
	for rows.Next() {
		var a, b int64
		if err = rows.Scan(&a, &b); err != nil {
			break
		}
		ia, ok := index[a]
		ib, okb := index[b]
		if !ok || !okb || ia == ib {
			continue
		}
		if len(edges) >= o.MaxEdges {
			err = fmt.Errorf("edge limit exceeded (%d); no partial snapshot published", o.MaxEdges)
			break
		}
		if err = check(192); err != nil {
			break
		}
		nodes[ia].out = append(nodes[ia].out, ib)
		nodes[ib].in = append(nodes[ib].in, ia)
		edges = append(edges, atlasLink{ia, ib})
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return
	}
	tx.Rollback()
	if o.RemoveOrphans {
		keep := make([]int, len(nodes))
		filtered := []atlasVertex{}
		for i, v := range nodes {
			keep[i] = -1
			if len(v.in)+len(v.out) > 0 {
				keep[i] = len(filtered)
				filtered = append(filtered, v)
			}
		}
		for i := range filtered {
			v := &filtered[i]
			for j, x := range v.in {
				v.in[j] = keep[x]
			}
			for j, x := range v.out {
				v.out[j] = keep[x]
			}
		}
		for i := range edges {
			edges[i].a = keep[edges[i].a]
			edges[i].b = keep[edges[i].b]
		}
		nodes = filtered
	}
	progress("Clustering and ranking")
	n := len(nodes)
	for i := range nodes {
		nodes[i].rank = 1 / math.Max(1, float64(n))
	}
	rng := rand.New(rand.NewSource(42))
	// Degree-normalized label propagation: linear, bounded passes; a hub's
	// thousands of leaves do not give it thousands of identical full votes.
	for pass := 0; pass < o.Passes; pass++ {
		if err = ctx.Err(); err != nil {
			return
		}
		next := make([]float64, n)
		dangling := 0.
		for i := range nodes {
			v := &nodes[i]
			if len(v.out) == 0 {
				dangling += v.rank
			} else {
				for _, j := range v.out {
					next[j] += .85 * v.rank / float64(len(v.out))
				}
			}
		}
		for i := range nodes {
			nodes[i].rank = next[i] + (.15+.85*dangling)/math.Max(1, float64(n))
		}
	}
	groups, e := clusterAtlas(ctx, nodes, o.ClusterSize, o.Passes)
	if e != nil {
		err = e
		return
	}
	progress("Laying out network")
	if err = layoutAtlas(ctx, nodes, edges, groups, o.Passes, rng); err != nil {
		return
	}
	k := len(groups)

	minx, miny, maxx, maxy := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, v := range nodes {
		minx = math.Min(minx, v.x)
		maxx = math.Max(maxx, v.x)
		miny = math.Min(miny, v.y)
		maxy = math.Max(maxy, v.y)
	}
	span := math.Max(maxx-minx, maxy-miny)
	if span < 1e-9 {
		span = 1
	}
	for i := range nodes {
		nodes[i].x = .5 + .96*(nodes[i].x-(minx+maxx)/2)/span
		nodes[i].y = .5 + .96*(nodes[i].y-(miny+maxy)/2)/span
	}
	progress("Writing spatial index")
	info.Hosts = n
	info.Edges = len(edges)
	info.Clusters = k
	temp := target + "." + id + ".tmp"
	defer os.Remove(temp)
	db, err := sql.Open("sqlite3", "file:"+temp+"?_journal_mode=DELETE&_synchronous=FULL")
	if err != nil {
		return
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `CREATE TABLE meta(value TEXT); CREATE TABLE nodes(id INTEGER PRIMARY KEY,name TEXT,known INTEGER,visited INTEGER,rank REAL,community INTEGER,x REAL,y REAL,incoming INTEGER,outgoing INTEGER,bridges INTEGER); CREATE TABLE edges(source INTEGER,target INTEGER,PRIMARY KEY(source,target)); CREATE INDEX incoming_edges ON edges(target,source); CREATE VIRTUAL TABLE spatial USING rtree(id,minx,maxx,miny,maxy);`)
	if err != nil {
		return
	}
	wt, err := db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer wt.Rollback()
	insert, err := wt.PrepareContext(ctx, "INSERT INTO nodes VALUES(?,?,?,?,?,?,?,?,?,?,?)")
	if err != nil {
		return
	}
	defer insert.Close()
	sp, err := wt.PrepareContext(ctx, "INSERT INTO spatial VALUES(?,?,?,?,?)")
	if err != nil {
		return
	}
	defer sp.Close()
	for i, v := range nodes {
		if i%4096 == 0 {
			if err = ctx.Err(); err != nil {
				return
			}
		}
		bridge := map[int]bool{}
		for _, adj := range [][]int{v.in, v.out} {
			for _, j := range adj {
				if nodes[j].cluster != v.cluster {
					bridge[nodes[j].cluster] = true
				}
			}
		}
		_, err = insert.ExecContext(ctx, v.id, v.name, v.known, v.visited, v.rank, v.cluster, v.x, v.y, len(v.in), len(v.out), len(bridge))
		if err != nil {
			return
		}
		if _, err = sp.ExecContext(ctx, v.id, v.x, v.x, v.y, v.y); err != nil {
			return
		}
	}
	es, err := wt.PrepareContext(ctx, "INSERT INTO edges VALUES(?,?)")
	if err != nil {
		return
	}
	defer es.Close()
	for _, e := range edges {
		if _, err = es.ExecContext(ctx, nodes[e.a].id, nodes[e.b].id); err != nil {
			return
		}
	}
	meta, _ := json.Marshal(info)
	if _, err = wt.ExecContext(ctx, "INSERT INTO meta VALUES(?)", string(meta)); err != nil {
		return
	}
	if err = wt.Commit(); err != nil {
		return
	}
	if err = buildAtlasRaster(ctx, db, info, progress); err != nil {
		return
	}
	if err = os.Chmod(temp, 0600); err != nil {
		return
	}
	if err = db.Close(); err != nil {
		return
	}
	if err = ctx.Err(); err != nil {
		return
	}
	err = os.Rename(temp, target)
	return
}
