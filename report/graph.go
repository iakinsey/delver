package report

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type topology struct {
	hosts       map[int64]Host
	ids         []int64
	out, in     map[int64][]int64
	rank        map[int64]float64
	communities map[int64]int
	bridge      map[int64]int
	revision    int64
	at          time.Time
}

func (g *topology) HasEdgeFromTo(a, b int64) bool {
	ids := g.out[a]
	i := sort.Search(len(ids), func(i int) bool { return ids[i] >= b })
	return i < len(ids) && ids[i] == b
}

// Cache only topology analysis, not live URL counts. One analysis runs at a time.
var analyses = struct {
	gate   chan struct{}
	values map[string]*topology
}{gate: make(chan struct{}, 1), values: map[string]*topology{}}

// Forget releases cached topology after storage deletion. The analysis gate also
// prevents an already-running query from publishing a deleted run afterward.
func Forget(path string) {
	analyses.gate <- struct{}{}
	defer func() { <-analyses.gate }()
	for key := range analyses.values {
		if key == path || strings.HasPrefix(key, path+string(filepath.Separator)) {
			delete(analyses.values, key)
		}
	}
}

func (s *Store) topology(ctx context.Context) (g *topology, err error) {
	counts, err := s.Counts(ctx)
	if err != nil {
		return nil, err
	}
	select {
	case analyses.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-analyses.gate }()
	if old := analyses.values[s.path]; old != nil && old.revision == counts.Revision {
		return old, nil
	}
	if counts.Hosts > 100000 || counts.Edges > 1000000 {
		return nil, fmt.Errorf("graph analysis limit exceeded: 100000 hosts or 1000000 links")
	}
	g = &topology{hosts: map[int64]Host{}, out: map[int64][]int64{}, in: map[int64][]int64{}, rank: map[int64]float64{}, communities: map[int64]int{}, bridge: map[int64]int{}, revision: counts.Revision, at: time.Now()}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Capture the revision inside the same read transaction as its topology.
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM meta WHERE id=1").Scan(&g.revision); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,name,known,visited FROM hosts ORDER BY id")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var h Host
		if err = rows.Scan(&h.ID, &h.Name, &h.Known, &h.Visited); err != nil {
			rows.Close()
			return nil, err
		}
		g.hosts[h.ID] = h
		g.ids = append(g.ids, h.ID)
		if len(g.ids) > 100000 {
			rows.Close()
			return nil, fmt.Errorf("graph analysis host limit exceeded")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, "SELECT source,target FROM edges ORDER BY source,target")
	if err != nil {
		return nil, err
	}
	n := 0
	for rows.Next() {
		var a, b int64
		if err = rows.Scan(&a, &b); err != nil {
			rows.Close()
			return nil, err
		}
		g.out[a] = append(g.out[a], b)
		g.in[b] = append(g.in[b], a)
		n++
		if n > 1000000 {
			rows.Close()
			return nil, fmt.Errorf("graph analysis edge limit exceeded")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	tx.Rollback()
	for id := range g.in {
		sort.Slice(g.in[id], func(i, j int) bool { return g.in[id][i] < g.in[id][j] })
	}
	if len(g.ids) > 0 {
		size := float64(len(g.ids))
		for _, id := range g.ids {
			g.rank[id] = 1 / size
		}
		for iteration := 0; iteration < 30; iteration++ {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			next := make(map[int64]float64, len(g.ids))
			dangling := 0.0
			for _, id := range g.ids {
				if len(g.out[id]) == 0 {
					dangling += g.rank[id]
				}
			}
			for _, id := range g.ids {
				next[id] = (.15 + .85*dangling) / size
			}
			for _, id := range g.ids {
				if len(g.out[id]) > 0 {
					v := .85 * g.rank[id] / float64(len(g.out[id]))
					for _, to := range g.out[id] {
						next[to] += v
					}
				}
			}
			g.rank = next
		}
		// Bounded, degree-normalized label propagation keeps community discovery
		// linear in the stored links. A high-degree hub cannot dominate every vote.
		order := append([]int64(nil), g.ids...)
		for i, id := range g.ids {
			g.communities[id] = i
		}
		rng := rand.New(rand.NewSource(1))
		for pass := 0; pass < 12; pass++ {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
			changed := false
			for _, id := range order {
				votes := map[int]float64{}
				for _, adj := range [][]int64{g.out[id], g.in[id]} {
					for _, v := range adj {
						votes[g.communities[v]] += 1 / math.Sqrt(float64(len(g.out[v])+len(g.in[v])))
					}
				}
				best := g.communities[id]
				score := votes[best]
				for label, vote := range votes {
					if vote > score || vote == score && label < best {
						best, score = label, vote
					}
				}
				if best != g.communities[id] {
					g.communities[id] = best
					changed = true
				}
			}
			if !changed {
				break
			}
		}
		for _, id := range g.ids {
			other := map[int]bool{}
			for _, adj := range [][]int64{g.out[id], g.in[id]} {
				for _, v := range adj {
					if g.communities[v] != g.communities[id] {
						other[g.communities[v]] = true
					}
				}
			}
			g.bridge[id] = len(other)
		}
	}
	if len(analyses.values) >= 2 {
		var oldest string
		var at time.Time
		for path, v := range analyses.values {
			if oldest == "" || v.at.Before(at) {
				oldest, at = path, v.at
			}
		}
		delete(analyses.values, oldest)
	}
	if _, statErr := os.Stat(s.path); statErr == nil {
		analyses.values[s.path] = g
	}
	return g, nil
}

// GraphOptions bounds rendering, not neighborhood depth. Limits are explicit.
type GraphOptions struct {
	Host      string   `json:"host"`
	Depth     int      `json:"depth"`
	Mode      string   `json:"mode"`
	NodeLimit int      `json:"node_limit"`
	EdgeLimit int      `json:"edge_limit"`
	Expanded  []string `json:"expanded"`
}
type GraphNode struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Group         bool    `json:"group"`
	Members       int     `json:"members"`
	Known         int64   `json:"known"`
	Visited       int64   `json:"visited"`
	Authority     float64 `json:"authority"`
	Percentile    float64 `json:"percentile"`
	Bridges       int     `json:"bridges"`
	Incoming      int     `json:"incoming"`
	Outgoing      int     `json:"outgoing"`
	Depth         int     `json:"depth"`
	MaxDepth      int     `json:"max_depth"`
	InternalLinks int     `json:"internal_links"`
	Selected      bool    `json:"selected"`
}
type GraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Links  int    `json:"links"`
}
type GraphView struct {
	Nodes      []GraphNode `json:"nodes"`
	Edges      []GraphEdge `json:"edges"`
	TotalHosts int         `json:"total_hosts"`
	TotalEdges int         `json:"total_edges"`
	Sampled    bool        `json:"sampled"`
	Grouped    bool        `json:"grouped"`
	Revision   int64       `json:"revision"`
	AnalyzedAt time.Time   `json:"analyzed_at"`
}

func (o *GraphOptions) validate() error {
	if o.Depth < 0 || o.Depth > 2147483647 {
		return fmt.Errorf("depth must be a nonnegative integer")
	}
	if o.NodeLimit == 0 {
		o.NodeLimit = 500
	}
	if o.EdgeLimit == 0 {
		o.EdgeLimit = 2000
	}
	if o.NodeLimit < 10 || o.NodeLimit > 2000 || o.EdgeLimit < 32 || o.EdgeLimit > 10000 {
		return fmt.Errorf("render limits: 10–2000 nodes, 32–10000 links")
	}
	if o.Mode == "" {
		o.Mode = "hosts"
	}
	if o.Mode != "hosts" && o.Mode != "grouped" {
		return fmt.Errorf("invalid graph mode")
	}
	if len(o.Expanded) > 64 {
		return fmt.Errorf("too many expanded groups")
	}
	return nil
}
func groupID(ids []int64) string {
	h := sha256.New()
	for _, id := range ids {
		fmt.Fprintf(h, "%d,", id)
	}
	return fmt.Sprintf("g:%x", h.Sum(nil)[:12])
}
func (g *topology) anchors(root int64, dist map[int64]int, parent map[int64]int64, limit int) map[int64]bool {
	selected := map[int64]bool{root: true}
	strata := map[string][]int64{}
	keys := []string{}
	for id, d := range dist {
		if id == root {
			continue
		}
		key := fmt.Sprintf("%010d:%010d", 2147483647-d, g.communities[id])
		if _, ok := strata[key]; !ok {
			keys = append(keys, key)
		}
		strata[key] = append(strata[key], id)
	}
	sort.Strings(keys)
	for _, key := range keys {
		sort.Slice(strata[key], func(i, j int) bool {
			a, b := strata[key][i], strata[key][j]
			if g.bridge[a] != g.bridge[b] {
				return g.bridge[a] > g.bridge[b]
			}
			if g.rank[a] != g.rank[b] {
				return g.rank[a] > g.rank[b]
			}
			return a < b
		})
	}
	for len(selected) < limit {
		progress := false
		for _, key := range keys {
			if len(strata[key]) == 0 {
				continue
			}
			id := strata[key][0]
			strata[key] = strata[key][1:]
			progress = true
			path := []int64{}
			for at := id; !selected[at]; at = parent[at] {
				path = append(path, at)
			}
			if len(path)+len(selected) <= limit {
				for _, v := range path {
					selected[v] = true
				}
			}
		}
		if !progress {
			break
		}
	}
	return selected
}
func (s *Store) Graph(ctx context.Context, o GraphOptions) (GraphView, error) {
	v := GraphView{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	if e := o.validate(); e != nil {
		return v, e
	}
	g, e := s.topology(ctx)
	if e != nil {
		return v, e
	}
	name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(o.Host)), ".")
	if strings.Contains(name, "://") {
		_, name, e = Normalize(name)
		if e != nil {
			return v, e
		}
	}
	var root int64
	for id, h := range g.hosts {
		if h.Name == name {
			root = id
			break
		}
	}
	if root == 0 {
		return v, fmt.Errorf("host not found")
	}
	dist := map[int64]int{root: 0}
	parent := map[int64]int64{}
	queue := []int64{root}
	for at := 0; at < len(queue); at++ {
		if e = ctx.Err(); e != nil {
			return v, e
		}
		id := queue[at]
		if dist[id] >= o.Depth {
			continue
		}
		for _, adj := range [][]int64{g.out[id], g.in[id]} {
			for _, to := range adj {
				if _, ok := dist[to]; !ok {
					dist[to] = dist[id] + 1
					parent[to] = id
					queue = append(queue, to)
				}
			}
		}
	}
	sort.Slice(queue, func(i, j int) bool { return queue[i] < queue[j] })
	totalEdges := 0
	for _, id := range queue {
		for _, to := range g.out[id] {
			if _, ok := dist[to]; ok {
				totalEdges++
			}
		}
	}
	v.TotalHosts = len(queue)
	v.TotalEdges = totalEdges
	v.Revision = g.revision
	v.AnalyzedAt = g.at
	hostBudget := o.NodeLimit
	if o.EdgeLimit+1 < hostBudget {
		hostBudget = o.EdgeLimit + 1
	}
	if o.Mode == "hosts" {
		for _, depth := range dist {
			if depth+1 > hostBudget {
				return v, fmt.Errorf("render limit cannot retain a path to depth %d; increase nodes or use Grouped", depth)
			}
		}
	}
	oversized := len(queue) > o.NodeLimit || totalEdges > o.EdgeLimit
	selected := map[int64]bool{}
	if !oversized {
		for _, id := range queue {
			selected[id] = true
		}
	} else {
		limit := hostBudget
		if o.Mode == "grouped" {
			limit = o.NodeLimit / 3
		}
		selected = g.anchors(root, dist, parent, limit)
	}
	groups := [][]int64{}
	protected := map[string]bool{}
	if oversized && o.Mode == "grouped" {
		by := map[string][]int64{}
		for _, id := range queue {
			if !selected[id] {
				key := fmt.Sprintf("%d:%d", dist[id], g.communities[id])
				by[key] = append(by[key], id)
			}
		}
		keys := []string{}
		for key := range by {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			groups = append(groups, by[key])
		}
		// Deterministic coarsening conserves every member; nothing is discarded.
		for len(groups)+len(selected) > o.NodeLimit {
			groups = mergeGroups(groups)
		}
		expanded := map[string]bool{}
		for _, id := range o.Expanded {
			expanded[id] = true
		}
		var split func([]int64, bool)
		split = func(ids []int64, revealed bool) {
			if expanded[groupID(ids)] && len(ids) > 1 {
				step := (len(ids) + 7) / 8
				for i := 0; i < len(ids); i += step {
					end := i + step
					if end > len(ids) {
						end = len(ids)
					}
					split(ids[i:end], true)
				}
			} else {
				groups = append(groups, ids)
				if revealed {
					protected[groupID(ids)] = true
				}
			}
		}
		original := groups
		groups = nil
		for _, ids := range original {
			split(ids, false)
		}
	}
	// Refresh URL attributes separately from cached topology/authority.
	hosts := map[int64]Host{}
	rows, e := s.db.QueryContext(ctx, "SELECT id,name,known,visited FROM hosts")
	if e != nil {
		return v, e
	}
	for rows.Next() {
		var h Host
		if e = rows.Scan(&h.ID, &h.Name, &h.Known, &h.Visited); e != nil {
			rows.Close()
			return v, e
		}
		hosts[h.ID] = h
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return v, e
	}
	ranks := make([]float64, 0, len(g.rank))
	for _, r := range g.rank {
		ranks = append(ranks, r)
	}
	sort.Float64s(ranks)
	build := func() {
		v.Nodes = []GraphNode{}
		v.Edges = []GraphEdge{}
		mapping := map[int64]string{}
		nodeIndex := map[string]int{}
		add := func(ids []int64, group bool) {
			id := fmt.Sprintf("h:%d", ids[0])
			if group && len(ids) > 1 {
				id = groupID(ids)
			} else {
				group = false
			}
			n := GraphNode{ID: id, Group: group, Members: len(ids), Depth: math.MaxInt32}
			for _, key := range ids {
				h := hosts[key]
				mapping[key] = id
				n.Known += h.Known
				n.Visited += h.Visited
				if dist[key] < n.Depth {
					n.Depth = dist[key]
				}
				if dist[key] > n.MaxDepth {
					n.MaxDepth = dist[key]
				}
				if !group {
					n.Name = h.Name
					n.Authority = g.rank[key]
					n.Percentile = 100 * float64(sort.SearchFloat64s(ranks, g.rank[key])) / float64(len(ranks))
					n.Bridges = g.bridge[key]
					n.Incoming = len(g.in[key])
					n.Outgoing = len(g.out[key])
					n.Selected = key == root
				}
			}
			if group {
				n.Name = fmt.Sprintf("%d hosts", len(ids))
			}
			nodeIndex[id] = len(v.Nodes)
			v.Nodes = append(v.Nodes, n)
		}
		for _, id := range queue {
			if selected[id] {
				add([]int64{id}, false)
			}
		}
		for _, ids := range groups {
			add(ids, true)
		}
		edges := map[[2]string]int{}
		for _, id := range queue {
			a, ok := mapping[id]
			if !ok {
				continue
			}
			for _, to := range g.out[id] {
				b, ok := mapping[to]
				if !ok {
					continue
				}
				if a == b {
					v.Nodes[nodeIndex[a]].InternalLinks++
					continue
				}
				edges[[2]string{a, b}]++
			}
		}
		keys := make([][2]string, 0, len(edges))
		for key := range edges {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i][0] != keys[j][0] {
				return keys[i][0] < keys[j][0]
			}
			return keys[i][1] < keys[j][1]
		})
		// Real BFS paths get priority before cross-community and remaining links.
		priority := map[[2]string]bool{}
		for id, p := range parent {
			a, oka := mapping[id]
			b, okb := mapping[p]
			if !oka || !okb || a == b {
				continue
			}
			if g.HasEdgeFromTo(p, id) {
				priority[[2]string{b, a}] = true
			} else {
				priority[[2]string{a, b}] = true
			}
		}
		sort.SliceStable(keys, func(i, j int) bool { return priority[keys[i]] && !priority[keys[j]] })
		for _, key := range keys {
			v.Edges = append(v.Edges, GraphEdge{key[0], key[1], edges[key]})
		}
	}
	build()
	if len(groups) > 0 {
		for len(v.Edges) > o.EdgeLimit || len(v.Nodes) > o.NodeLimit {
			if merged, ok := mergeUntouched(groups, protected); ok {
				groups = merged
				build()
				continue
			}
			candidates := make([]int64, 0, len(selected))
			for id := range selected {
				if id != root {
					candidates = append(candidates, id)
				}
			}
			if len(candidates) == 0 {
				return GraphView{}, fmt.Errorf("expansion exceeds render limits; increase rendered nodes or links")
			}
			sort.Slice(candidates, func(i, j int) bool {
				a, b := candidates[i], candidates[j]
				if g.bridge[a] != g.bridge[b] {
					return g.bridge[a] < g.bridge[b]
				}
				if g.rank[a] != g.rank[b] {
					return g.rank[a] < g.rank[b]
				}
				return a > b
			})
			// Fold a proportional batch rather than rescanning all links once
			// for every anchor in a dense neighborhood.
			count := 1
			if len(v.Edges) > o.EdgeLimit {
				count = int(float64(len(selected)) * (1 - math.Sqrt(float64(o.EdgeLimit)/float64(len(v.Edges)))))
				if count < 1 {
					count = 1
				}
			}
			if over := len(v.Nodes) - o.NodeLimit; over > count {
				count = over
			}
			if count > len(candidates) {
				count = len(candidates)
			}
			folded := candidates[:count]
			for _, id := range folded {
				delete(selected, id)
			}
			target := -1
			for i, ids := range groups {
				if !protected[groupID(ids)] {
					target = i
					break
				}
			}
			if target < 0 {
				groups = append(groups, append([]int64(nil), folded...))
			} else {
				groups[target] = append(groups[target], folded...)
			}
			for _, ids := range groups {
				sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
			}
			if e = ctx.Err(); e != nil {
				return v, e
			}

			build()
		}
	} else if len(v.Edges) > o.EdgeLimit {
		v.Edges = v.Edges[:o.EdgeLimit]
	}

	v.Grouped = len(groups) > 0
	v.Sampled = !v.Grouped && (len(v.Nodes) < v.TotalHosts || len(v.Edges) < v.TotalEdges)
	return v, nil
}
func mergeGroups(groups [][]int64) [][]int64 {
	out := [][]int64{}
	for i := 0; i < len(groups); i += 2 {
		ids := append([]int64{}, groups[i]...)
		if i+1 < len(groups) {
			ids = append(ids, groups[i+1]...)
		}
		sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
		out = append(out, ids)
	}
	return out
}

func mergeUntouched(groups [][]int64, protected map[string]bool) ([][]int64, bool) {
	out := make([][]int64, 0, len(groups))
	pending := []int64(nil)
	merged := false
	for _, ids := range groups {
		if protected[groupID(ids)] {
			out = append(out, ids)
			continue
		}
		if pending == nil {
			pending = ids
			continue
		}
		joined := append(append([]int64{}, pending...), ids...)
		sort.Slice(joined, func(i, j int) bool { return joined[i] < joined[j] })
		out = append(out, joined)
		pending = nil
		merged = true
	}
	if pending != nil {
		out = append(out, pending)
	}
	return out, merged
}
