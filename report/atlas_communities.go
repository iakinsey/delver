package report

import (
	"context"
	"math"
	"math/rand"
	"sort"
)

// Weighted CSR keeps neighborhood scans linear in edges. In particular, a
// popular host must not cause every candidate move to scan an entire community.
type atlasModGraph struct {
	offset, peer         []int
	weight, self, degree []float64
	total                float64
}

func atlasModInput(ctx context.Context, nodes []atlasVertex) (atlasModGraph, error) {
	g := atlasModGraph{offset: make([]int, len(nodes)+1), self: make([]float64, len(nodes)), degree: make([]float64, len(nodes))}
	for v, n := range nodes {
		if v%1024 == 0 {
			if e := ctx.Err(); e != nil {
				return g, e
			}
		}
		i, j := 0, 0
		for i < len(n.out) || j < len(n.in) {
			var p int
			w := 1.
			switch {
			case j == len(n.in) || (i < len(n.out) && n.out[i] < n.in[j]):
				p = n.out[i]
				i++
			case i == len(n.out) || n.in[j] < n.out[i]:
				p = n.in[j]
				j++
			default:
				p = n.out[i]
				i++
				j++
				w = 2
			}
			g.peer = append(g.peer, p)
			g.weight = append(g.weight, w)
			g.degree[v] += w
			g.total += w
		}
		g.offset[v+1] = len(g.peer)
	}
	return g, nil
}

// Louvain local moving with cached community totals and neighbor-community
// weights. Gain is k(i,C)-gamma*k(i)*sumTot(C)/2m, evaluated after removal.
// Each pass is O(vertices+edges); both pass count and hierarchy depth are bounded.
func atlasModularize(ctx context.Context, g atlasModGraph, resolution float64, passes int) ([]int, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	assignment := make([]int, len(g.degree))
	for i := range assignment {
		assignment[i] = i
	}
	rng := rand.New(rand.NewSource(42))
	for level := 0; level < 16 && len(g.degree) > 0; level++ {
		n := len(g.degree)
		label := make([]int, n)
		order := make([]int, n)
		totals := append([]float64(nil), g.degree...)
		votes := make([]float64, n)
		seen := make([]uint32, n)
		stamp := uint32(0)
		candidates := make([]int, 0)
		for i := range label {
			label[i] = i
			order[i] = i
		}
		if g.total == 0 {
			return assignment, ctx.Err()
		}
		movedAny := false
		for pass := 0; pass < passes; pass++ {
			rng.Shuffle(n, func(i, j int) { order[i], order[j] = order[j], order[i] })
			moved := 0
			for k, v := range order {
				if k%1024 == 0 {
					if e := ctx.Err(); e != nil {
						return nil, e
					}
				}
				old := label[v]
				degree := g.degree[v]
				if degree == 0 {
					continue
				}
				totals[old] -= degree
				stamp++
				candidates = candidates[:0]
				for e := g.offset[v]; e < g.offset[v+1]; e++ {
					c := label[g.peer[e]]
					if seen[c] != stamp {
						seen[c] = stamp
						votes[c] = 0
						candidates = append(candidates, c)
					}
					votes[c] += g.weight[e]
				}
				own := 0.
				if seen[old] == stamp {
					own = votes[old]
				}
				best, bestGain := old, own-resolution*degree*totals[old]/g.total
				originalGain := bestGain
				for _, c := range candidates {
					gain := votes[c] - resolution*degree*totals[c]/g.total
					if gain > bestGain+1e-12 || (gain > originalGain+1e-12 && math.Abs(gain-bestGain) <= 1e-12 && c < best) {
						best, bestGain = c, gain
					}
				}
				label[v] = best
				totals[best] += degree
				if best != old {
					moved++
					movedAny = true
				}
			}
			if moved == 0 {
				break
			}
		}
		dense := make([]int, n)
		for i := range dense {
			dense[i] = -1
		}
		count := 0
		for i := range label {
			c := label[i]
			if dense[c] < 0 {
				dense[c] = count
				count++
			}
			label[i] = dense[c]
		}
		for i, a := range assignment {
			assignment[i] = label[a]
		}
		if !movedAny || count == n {
			break
		}
		next, e := atlasModReduce(ctx, g, label, count)
		if e != nil {
			return nil, e
		}
		g = next
	}
	return assignment, ctx.Err()
}
func atlasModReduce(ctx context.Context, g atlasModGraph, label []int, n int) (atlasModGraph, error) {
	next := atlasModGraph{offset: make([]int, n+1), self: make([]float64, n), degree: make([]float64, n), total: g.total}
	weights := make(map[[2]int]float64)
	for v, c := range label {
		if v%1024 == 0 {
			if e := ctx.Err(); e != nil {
				return next, e
			}
		}
		next.self[c] += g.self[v]
		next.degree[c] += g.degree[v]
		for e := g.offset[v]; e < g.offset[v+1]; e++ {
			other := label[g.peer[e]]
			if c == other {
				next.self[c] += g.weight[e]
			} else {
				weights[[2]int{c, other}] += g.weight[e]
			}
		}
	}
	keys := make([][2]int, 0, len(weights))
	for k := range weights {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i][0] < keys[j][0] || keys[i][0] == keys[j][0] && keys[i][1] < keys[j][1]
	})
	for _, k := range keys {
		next.offset[k[0]+1]++
		next.peer = append(next.peer, k[1])
		next.weight = append(next.weight, weights[k])
	}
	for i := 1; i < len(next.offset); i++ {
		next.offset[i] += next.offset[i-1]
	}
	return next, ctx.Err()
}
func atlasModSubset(ctx context.Context, g atlasModGraph, ids []int) (atlasModGraph, error) {
	position := make(map[int]int, len(ids))
	for i, id := range ids {
		position[id] = i
	}
	sub := atlasModGraph{offset: make([]int, len(ids)+1), self: make([]float64, len(ids)), degree: make([]float64, len(ids))}
	for i, id := range ids {
		if i%1024 == 0 {
			if e := ctx.Err(); e != nil {
				return sub, e
			}
		}
		sub.self[i] = g.self[id]
		sub.degree[i] = sub.self[i]
		for e := g.offset[id]; e < g.offset[id+1]; e++ {
			if j, ok := position[g.peer[e]]; ok {
				sub.peer = append(sub.peer, j)
				sub.weight = append(sub.weight, g.weight[e])
				sub.degree[i] += g.weight[e]
			}
		}
		sub.offset[i+1] = len(sub.peer)
		sub.total += sub.degree[i]
	}
	return sub, nil
}
func atlasModGroups(labels, ids []int) [][]int {
	size := 0
	for _, l := range labels {
		size = max(size, l+1)
	}
	groups := make([][]int, size)
	for i, l := range labels {
		groups[l] = append(groups[l], ids[i])
	}
	return groups
}
func clusterAtlas(ctx context.Context, nodes []atlasVertex, limit int, analysis ...int) ([][]int, error) {
	passes := 12
	if len(analysis) > 0 {
		passes = analysis[0]
	}
	graph, e := atlasModInput(ctx, nodes)
	if e != nil {
		return nil, e
	}
	labels, e := atlasModularize(ctx, graph, 1, passes)
	if e != nil {
		return nil, e
	}
	ids := make([]int, len(nodes))
	for i := range ids {
		ids[i] = i
	}
	groups := [][]int{}
	var refine func([]int, int) error
	refine = func(ids []int, depth int) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if len(ids) <= limit {
			sort.Ints(ids)
			groups = append(groups, ids)
			return nil
		}
		sub, e := atlasModSubset(ctx, graph, ids)
		if e != nil {
			return e
		}
		labels, e := atlasModularize(ctx, sub, 2, passes)
		if e != nil {
			return e
		}
		parts := atlasModGroups(labels, ids)
		if len(parts) > 1 && depth < 8 {
			for _, p := range parts {
				if e = refine(p, depth+1); e != nil {
					return e
				}
			}
			return nil
		}
		// An indivisible star still needs bounded display partitions. No graph
		// vertices or edges are omitted by this final storage/layout bound.
		for start := 0; start < len(ids); start += limit {
			if e = refine(ids[start:min(len(ids), start+limit)], 9); e != nil {
				return e
			}
		}
		return nil
	}
	for _, p := range atlasModGroups(labels, ids) {
		if e = refine(p, 0); e != nil {
			return nil, e
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i]) != len(groups[j]) {
			return len(groups[i]) > len(groups[j])
		}
		return groups[i][0] < groups[j][0]
	})
	for c, g := range groups {
		for _, i := range g {
			nodes[i].cluster = c
		}
	}
	return groups, nil
}
