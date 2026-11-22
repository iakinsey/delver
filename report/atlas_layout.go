package report

import (
	"context"
	"math"
	"math/rand"
	"sort"

	"gonum.org/v1/gonum/spatial/barneshut"
	"gonum.org/v1/gonum/spatial/r2"
)

type atlasSpring struct {
	a, b   int
	weight float64
}
type atlasParticle struct{ position r2.Vec }

func (p *atlasParticle) Coord2() r2.Vec { return p.position }
func (p *atlasParticle) Mass() float64  { return 1 }

// Fruchterman-Reingold with Barnes-Hut repulsion: real edges exert attraction,
// every vertex repels, and cooling bounds each step. No spatial buckets or
// viewport-dependent coordinates participate in the generated geometry.
func atlasForce(ctx context.Context, n int, edges []atlasSpring, steps int, rng *rand.Rand) ([][2]float64, error) {
	points := make([]atlasParticle, n)
	particles := make([]barneshut.Particle2, n)
	scale := math.Sqrt(float64(max(n, 1)))
	for i := range points {
		points[i].position = r2.Vec{X: (rng.Float64() - .5) * scale, Y: (rng.Float64() - .5) * scale}
		particles[i] = &points[i]
	}
	force := make([]r2.Vec, n)
	repel := func(_, _ barneshut.Particle2, m1, m2 float64, v r2.Vec) r2.Vec {
		d := math.Max(1e-8, v.X*v.X+v.Y*v.Y)
		return r2.Scale(-m1*m2/d, v)
	}
	for pass := 0; pass < steps && n > 1; pass++ {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		plane, e := barneshut.NewPlane(particles)
		if e != nil {
			return nil, e
		}
		for i := range points {
			force[i] = plane.ForceOn(particles[i], .7, repel)
			force[i].X -= points[i].position.X * .015
			force[i].Y -= points[i].position.Y * .015
		}
		for _, edge := range edges {
			v := r2.Sub(points[edge.b].position, points[edge.a].position)
			d := math.Max(1e-6, math.Hypot(v.X, v.Y))
			f := r2.Scale(d*edge.weight, v)
			force[edge.a] = r2.Add(force[edge.a], f)
			force[edge.b] = r2.Sub(force[edge.b], f)
		}
		temperature := .025 + scale*.10*math.Pow(1-float64(pass)/float64(steps), 2)
		for i, f := range force {
			length := math.Hypot(f.X, f.Y)
			if length > 0 {
				points[i].position = r2.Add(points[i].position, r2.Scale(math.Min(length, temperature)/length, f))
			}
		}
	}
	out := make([][2]float64, n)
	for i, p := range points {
		out[i] = [2]float64{p.position.X, p.position.Y}
	}
	return out, nil
}

func layoutAtlas(ctx context.Context, nodes []atlasVertex, edges []atlasLink, groups [][]int, passes int, rng *rand.Rand) error {
	if len(nodes) == 0 {
		return nil
	}
	counts := map[[2]int]int{}
	localEdges := make([][]atlasSpring, len(groups))
	indices := make([]int, len(nodes))
	for _, g := range groups {
		for j, i := range g {
			indices[i] = j
		}
	}
	for _, e := range edges {
		a, b := nodes[e.a].cluster, nodes[e.b].cluster
		if a == b {
			localEdges[a] = append(localEdges[a], atlasSpring{indices[e.a], indices[e.b], 1})
		} else {
			if a > b {
				a, b = b, a
			}
			counts[[2]int{a, b}]++
		}
	}
	springs := make([]atlasSpring, 0, len(counts))
	for p, n := range counts {
		springs = append(springs, atlasSpring{p[0], p[1], math.Min(4, math.Log1p(float64(n)))})
	}
	sort.Slice(springs, func(i, j int) bool { a, b := springs[i], springs[j]; return a.a < b.a || a.a == b.a && a.b < b.b })
	radii := make([]float64, len(groups))
	average := float64(len(nodes)) / float64(len(groups))
	for c, g := range groups {
		radii[c] = math.Max(.18, .65*math.Sqrt(float64(len(g))/average))
	}
	centers, e := layoutAtlasComponents(ctx, radii, springs, 120+passes*4, rng)
	if e != nil {
		return e
	}
	for c, g := range groups {
		local, e := atlasForce(ctx, len(g), localEdges[c], 48+passes*4, rng)
		if e != nil {
			return e
		}
		cx, cy := 0., 0.
		for _, p := range local {
			cx += p[0]
			cy += p[1]
		}
		cx /= float64(len(g))
		cy /= float64(len(g))
		spread := 1e-6
		for _, p := range local {
			spread = math.Max(spread, math.Hypot(p[0]-cx, p[1]-cy))
		}
		degree := make([]int, len(g))
		for _, edge := range localEdges[c] {
			degree[edge.a]++
			degree[edge.b]++
		}
		extent := 1e-6
		for j := range g {
			if len(g) > 30 && degree[j] <= 4 {
				local[j][0] += rng.NormFloat64() * spread * .18
				local[j][1] += rng.NormFloat64() * spread * .18
			}
			extent = math.Max(extent, math.Hypot(local[j][0]-cx, local[j][1]-cy))
		}
		for j, i := range g {
			nodes[i].x = centers[c][0] + (local[j][0]-cx)/extent*radii[c]
			nodes[i].y = centers[c][1] + (local[j][1]-cy)/extent*radii[c]
		}
	}
	return nil
}

func separateAtlasCenters(ctx context.Context, centers [][2]float64, radii []float64) error {
	// Radius-aware collision relaxation preserves the quotient layout while
	// preventing densely connected communities from collapsing into one disk.
	maxRadius := 0.
	for _, r := range radii {
		maxRadius = math.Max(maxRadius, r)
	}
	cell := math.Max(.5, maxRadius*2.2)
	for pass := 0; pass < 70; pass++ {
		if e := ctx.Err(); e != nil {
			return e
		}
		cells := map[[2]int][]int{}
		for i, p := range centers {
			k := [2]int{int(math.Floor(p[0] / cell)), int(math.Floor(p[1] / cell))}
			cells[k] = append(cells[k], i)
		}
		for i, p := range centers {
			k := [2]int{int(math.Floor(p[0] / cell)), int(math.Floor(p[1] / cell))}
			for x := -1; x <= 1; x++ {
				for y := -1; y <= 1; y++ {
					for _, j := range cells[[2]int{k[0] + x, k[1] + y}] {
						if j <= i {
							continue
						}
						dx, dy := centers[j][0]-centers[i][0], centers[j][1]-centers[i][1]
						d := math.Hypot(dx, dy) + 1e-9
						over := (radii[i]+radii[j])*1.12 - d
						if over > 0 {
							move := over * .25
							centers[i][0] -= dx / d * move
							centers[i][1] -= dy / d * move
							centers[j][0] += dx / d * move
							centers[j][1] += dy / d * move
						}
					}
				}
			}
		}
	}
	return nil
}

type atlasComponentBox struct {
	members                   []int
	points                    [][2]float64
	minx, miny, width, height float64
	root                      int
}

func layoutAtlasComponents(ctx context.Context, radii []float64, springs []atlasSpring, steps int, rng *rand.Rand) ([][2]float64, error) {
	parent := make([]int, len(radii))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	for _, e := range springs {
		a, b := find(e.a), find(e.b)
		if a != b {
			parent[b] = a
		}
	}
	members := map[int][]int{}
	for i := range parent {
		r := find(i)
		members[r] = append(members[r], i)
	}
	keys := make([]int, 0, len(members))
	for k := range members {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	localIndex := make([]int, len(radii))
	for _, g := range members {
		for j, i := range g {
			localIndex[i] = j
		}
	}
	edgeLists := map[int][]atlasSpring{}
	for _, e := range springs {
		r := find(e.a)
		edgeLists[r] = append(edgeLists[r], atlasSpring{localIndex[e.a], localIndex[e.b], e.weight})
	}
	boxes := make([]atlasComponentBox, 0, len(keys))
	area, maxWidth := 0., 0.
	for _, root := range keys {
		g := members[root]
		points, e := atlasForce(ctx, len(g), edgeLists[root], steps, rng)
		if e != nil {
			return nil, e
		}
		r := make([]float64, len(g))
		for j, i := range g {
			r[j] = radii[i]
		}
		if e = separateAtlasCenters(ctx, points, r); e != nil {
			return nil, e
		}
		box := atlasComponentBox{members: g, points: points, minx: math.Inf(1), miny: math.Inf(1), root: root}
		maxx, maxy := math.Inf(-1), math.Inf(-1)
		for j, p := range points {
			box.minx = math.Min(box.minx, p[0]-r[j])
			box.miny = math.Min(box.miny, p[1]-r[j])
			maxx = math.Max(maxx, p[0]+r[j])
			maxy = math.Max(maxy, p[1]+r[j])
		}
		box.width = maxx - box.minx + .4
		box.height = maxy - box.miny + .4
		area += box.width * box.height
		maxWidth = math.Max(maxWidth, box.width)
		boxes = append(boxes, box)
	}
	sort.Slice(boxes, func(i, j int) bool {
		a, b := boxes[i], boxes[j]
		if a.height != b.height {
			return a.height > b.height
		}
		return a.root < b.root
	})
	width := math.Max(maxWidth, math.Sqrt(area)*1.05)
	x, y, rowHeight := 0., 0., 0.
	centers := make([][2]float64, len(radii))
	for _, b := range boxes {
		if x > 0 && x+b.width > width {
			x = 0
			y += rowHeight
			rowHeight = 0
		}
		for j, i := range b.members {
			centers[i] = [2]float64{x + b.points[j][0] - b.minx + .2, y + b.points[j][1] - b.miny + .2}
		}
		x += b.width
		rowHeight = math.Max(rowHeight, b.height)
	}
	return centers, nil
}
