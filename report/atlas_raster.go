package report

import (
	"bytes"
	"container/list"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strings"
	"sync"
)

const atlasTileSize = 512
const atlasTileBytes = 32 * 1024 * 1024

var atlasTilePermits = make(chan struct{}, 2)
var atlasTiles = struct {
	sync.Mutex
	order  *list.List
	items  map[string]*list.Element
	bytes  int
	epochs map[string]uint64
}{order: list.New(), items: map[string]*list.Element{}, epochs: map[string]uint64{}}

type atlasCachedTile struct {
	key  string
	data []byte
}

func cachedAtlasTile(key string) []byte {
	atlasTiles.Lock()
	defer atlasTiles.Unlock()
	if e := atlasTiles.items[key]; e != nil {
		atlasTiles.order.MoveToFront(e)
		return e.Value.(atlasCachedTile).data
	}
	return nil
}
func cacheAtlasTile(key string, data []byte, path string, epoch uint64) {
	if len(data) > atlasTileBytes {
		return
	}
	atlasTiles.Lock()
	defer atlasTiles.Unlock()
	if atlasTiles.epochs[path] != epoch {
		return
	}
	if atlasTiles.items[key] != nil {
		return
	}
	e := atlasTiles.order.PushFront(atlasCachedTile{key, data})
	atlasTiles.items[key] = e
	atlasTiles.bytes += len(data)
	for atlasTiles.bytes > atlasTileBytes || atlasTiles.order.Len() > 256 {
		e := atlasTiles.order.Back()
		v := e.Value.(atlasCachedTile)
		delete(atlasTiles.items, v.key)
		atlasTiles.bytes -= len(v.data)
		atlasTiles.order.Remove(e)
	}
}

type AtlasTileRequest struct {
	Snapshot     string `json:"snapshot"`
	Z            int    `json:"z"`
	X            int    `json:"x"`
	Y            int    `json:"y"`
	SelectedHost int64  `json:"selected_host"`
}
type AtlasTileImage struct {
	Snapshot string `json:"snapshot"`
	Z        int    `json:"z"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	PNG      []byte `json:"png"`
}

func atlasInfoTx(ctx context.Context, tx *sql.Tx, snapshot string) (info AtlasInfo, err error) {
	var raw string
	if err = tx.QueryRowContext(ctx, "SELECT value FROM meta").Scan(&raw); err != nil {
		return
	}
	if err = json.Unmarshal([]byte(raw), &info); err != nil {
		return
	}
	if info.ID != snapshot {
		return info, fmt.Errorf("snapshot changed; reload graph status")
	}
	if info.Format != 2 {
		return info, fmt.Errorf("regenerate graph snapshot to update its layout")
	}
	return
}

func buildAtlasRaster(ctx context.Context, db *sql.DB, info AtlasInfo, progress func(string)) error {
	progress("Indexing network geometry")
	_, e := db.ExecContext(ctx, `CREATE VIRTUAL TABLE edge_spatial USING rtree(id,minx,maxx,miny,maxy); INSERT INTO edge_spatial SELECT e.rowid,min(a.x,b.x),max(a.x,b.x),min(a.y,b.y),max(a.y,b.y) FROM edges e JOIN nodes a ON a.id=e.source JOIN nodes b ON b.id=e.target; CREATE INDEX node_authority ON nodes(rank DESC,id); CREATE TABLE tiles(z INTEGER,x INTEGER,y INTEGER,png BLOB,PRIMARY KEY(z,x,y)) WITHOUT ROWID;`)
	if e != nil {
		return e
	}
	progress("Rendering network overview")
	tx, e := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return e
	}
	img, e := renderAtlasTile(ctx, tx, info, AtlasTileRequest{}, nil)
	tx.Rollback()
	if e != nil {
		return e
	}
	var b bytes.Buffer
	if e = png.Encode(&b, img); e != nil {
		return e
	}
	_, e = db.ExecContext(ctx, "INSERT INTO tiles VALUES(0,0,0,?)", b.Bytes())
	return e
}
func AtlasTile(ctx context.Context, path string, o AtlasTileRequest) (out AtlasTileImage, err error) {
	atlasTiles.Lock()
	epoch := atlasTiles.epochs[path]
	atlasTiles.Unlock()
	if o.Z < 0 || o.Z > 16 || o.X < 0 || o.Y < 0 || o.X >= 1<<uint(o.Z) || o.Y >= 1<<uint(o.Z) || o.SelectedHost < 0 {
		return out, fmt.Errorf("invalid graph tile")
	}
	select {
	case atlasTilePermits <- struct{}{}:
		defer func() { <-atlasTilePermits }()
	case <-ctx.Done():
		return out, ctx.Err()
	}
	db, e := openAtlas(path)
	if e != nil {
		return out, e
	}
	defer db.Close()
	tx, e := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	info, e := atlasInfoTx(ctx, tx, o.Snapshot)
	if e != nil {
		return out, e
	}
	out = AtlasTileImage{Snapshot: info.ID, Z: o.Z, X: o.X, Y: o.Y}
	key := fmt.Sprintf("%s:%s:%d:%d:%d", path, info.ID, o.Z, o.X, o.Y)
	base := cachedAtlasTile(key)
	if base == nil {
		if o.Z == 0 {
			e = tx.QueryRowContext(ctx, "SELECT png FROM tiles WHERE z=0 AND x=0 AND y=0").Scan(&base)
		} else {
			var img *image.RGBA
			plain := o
			plain.SelectedHost = 0
			img, e = renderAtlasTile(ctx, tx, info, plain, nil)
			if e == nil {
				var b bytes.Buffer
				e = png.Encode(&b, img)
				base = b.Bytes()
			}
		}
		if e != nil {
			return out, e
		}
		cacheAtlasTile(key, base, path, epoch)
	}
	if o.SelectedHost == 0 {
		out.PNG = base
		return out, nil
	}
	var exists int
	if e = tx.QueryRowContext(ctx, "SELECT 1 FROM nodes WHERE id=?", o.SelectedHost).Scan(&exists); e != nil {
		return out, e
	}
	decoded, e := png.Decode(bytes.NewReader(base))
	if e != nil {
		return out, e
	}
	img := image.NewRGBA(image.Rect(0, 0, atlasTileSize, atlasTileSize))
	draw.Draw(img, img.Bounds(), decoded, image.Point{}, draw.Src)
	img, e = renderAtlasTile(ctx, tx, info, o, img)
	if e != nil {
		return out, e
	}
	var b bytes.Buffer
	if e = png.Encode(&b, img); e != nil {
		return out, e
	}
	out.PNG = b.Bytes()
	return out, nil
}

func atlasCommunityColor(c int) color.RGBA {
	h := math.Mod(float64(c)*137.508, 360) / 60
	s, l := .65, .65
	chroma := (1 - math.Abs(2*l-1)) * s
	x := chroma * (1 - math.Abs(math.Mod(h, 2)-1))
	m := l - chroma/2
	r, g, b := 0., 0., 0.
	switch {
	case h < 1:
		r, g = chroma, x
	case h < 2:
		r, g = x, chroma
	case h < 3:
		g, b = chroma, x
	case h < 4:
		g, b = x, chroma
	case h < 5:
		r, b = x, chroma
	default:
		r, b = chroma, x
	}
	return color.RGBA{uint8((r + m) * 255), uint8((g + m) * 255), uint8((b + m) * 255), 255}
}
func atlasBlend(img *image.RGBA, x, y int, c color.RGBA) {
	if x < 0 || y < 0 || x >= atlasTileSize || y >= atlasTileSize {
		return
	}
	i := y*img.Stride + x*4
	a := uint32(c.A)
	for j, v := range []uint8{c.R, c.G, c.B} {
		img.Pix[i+j] = uint8((uint32(v)*a + uint32(img.Pix[i+j])*(255-a) + 127) / 255)
	}
	img.Pix[i+3] = 255
}
func atlasLine(img *image.RGBA, x0, y0, x1, y1 float64, c color.RGBA) {
	// Liang-Barsky clipping ensures a long cross-network edge never walks pixels
	// outside this tile, including when both endpoints lie outside the viewport.
	dx, dy := x1-x0, y1-y0
	t0, t1 := 0., 1.
	for _, v := range [][2]float64{{-dx, x0}, {dx, float64(atlasTileSize-1) - x0}, {-dy, y0}, {dy, float64(atlasTileSize-1) - y0}} {
		if v[0] == 0 {
			if v[1] < 0 {
				return
			}
			continue
		}
		r := v[1] / v[0]
		if v[0] < 0 {
			t0 = math.Max(t0, r)
		} else {
			t1 = math.Min(t1, r)
		}
		if t0 > t1 {
			return
		}
	}
	x, y := int(math.Round(x0+t0*dx)), int(math.Round(y0+t0*dy))
	endx, endy := int(math.Round(x0+t1*dx)), int(math.Round(y0+t1*dy))
	ax, ay := int(math.Abs(float64(endx-x))), -int(math.Abs(float64(endy-y)))
	sx, sy := -1, -1
	if x < endx {
		sx = 1
	}
	if y < endy {
		sy = 1
	}
	e := ax + ay
	for {
		atlasBlend(img, x, y, c)
		if x == endx && y == endy {
			break
		}
		twice := e * 2
		if twice >= ay {
			e += ay
			x += sx
		}
		if twice <= ax {
			e += ax
			y += sy
		}
	}
}
func atlasDot(img *image.RGBA, x, y, r float64, c color.RGBA) {
	for yy := int(math.Floor(y - r - 1)); yy <= int(math.Ceil(y+r+1)); yy++ {
		for xx := int(math.Floor(x - r - 1)); xx <= int(math.Ceil(x+r+1)); xx++ {
			coverage := math.Max(0, math.Min(1, r+.5-math.Hypot(float64(xx)+.5-x, float64(yy)+.5-y)))
			v := c
			v.A = uint8(float64(c.A) * coverage)
			if v.A > 0 {
				atlasBlend(img, xx, yy, v)
			}
		}
	}
}
func renderAtlasTile(ctx context.Context, tx *sql.Tx, info AtlasInfo, o AtlasTileRequest, img *image.RGBA) (*image.RGBA, error) {
	if img == nil {
		img = image.NewRGBA(image.Rect(0, 0, atlasTileSize, atlasTileSize))
		draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{9, 15, 25, 255}), image.Point{}, draw.Src)
	}
	grid := float64(uint64(1) << uint(o.Z))
	scale := grid * atlasTileSize
	ox, oy := float64(o.X)*atlasTileSize, float64(o.Y)*atlasTileSize
	margin := 8 / scale
	minx, miny, maxx, maxy := float64(o.X)/grid-margin, float64(o.Y)/grid-margin, float64(o.X+1)/grid+margin, float64(o.Y+1)/grid+margin
	query := `SELECT a.x,a.y,b.x,b.y,e.source FROM edge_spatial r JOIN edges e ON e.rowid=r.id JOIN nodes a ON a.id=e.source JOIN nodes b ON b.id=e.target WHERE r.maxx>=? AND r.maxy>=? AND r.minx<=? AND r.miny<=?`
	args := []interface{}{minx, miny, maxx, maxy}
	if o.SelectedHost > 0 {
		query += " AND (e.source=? OR e.target=?)"
		args = append(args, o.SelectedHost, o.SelectedHost)
	}
	rows, e := tx.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, e
	}
	count := 0
	// Sparse networks need individually visible edges; dense networks retain
	// low opacity so overlapping connections reveal structure without saturation.
	edgeAlpha := math.Min(90, math.Max(2, 90/math.Sqrt(float64(max(1, info.Edges))/100))+float64(2*o.Z))
	for rows.Next() {
		if count%4096 == 0 {
			if e = ctx.Err(); e != nil {
				break
			}
		}
		count++
		var ax, ay, bx, by float64
		var source int64
		if e = rows.Scan(&ax, &ay, &bx, &by, &source); e != nil {
			break
		}
		x0, y0, x1, y1 := ax*scale-ox, ay*scale-oy, bx*scale-ox, by*scale-oy
		c := color.RGBA{95, 114, 140, uint8(edgeAlpha)}
		if o.SelectedHost > 0 {
			c = color.RGBA{71, 184, 255, 255}
			if source == o.SelectedHost {
				c = color.RGBA{255, 173, 87, 255}
			}
			dx, dy := x1-x0, y1-y0
			length := math.Hypot(dx, dy)
			if length > 0 {
				nx, ny := -dy/length*1.5, dx/length*1.5
				x0 += nx
				x1 += nx
				y0 += ny
				y1 += ny
			}
			angle := math.Atan2(y1-y0, x1-x0)
			x, y := x0+(x1-x0)*.68, y0+(y1-y0)*.68
			atlasLine(img, x, y, x-5*math.Cos(angle-.5), y-5*math.Sin(angle-.5), c)
			atlasLine(img, x, y, x-5*math.Cos(angle+.5), y-5*math.Sin(angle+.5), c)
		}
		atlasLine(img, x0, y0, x1, y1, c)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return nil, e
	}
	if o.SelectedHost > 0 {
		return img, nil
	}
	rows, e = tx.QueryContext(ctx, `SELECT n.x,n.y,n.rank,n.community FROM spatial r JOIN nodes n ON n.id=r.id WHERE r.maxx>=? AND r.maxy>=? AND r.minx<=? AND r.miny<=?`, minx, miny, maxx, maxy)
	if e != nil {
		return nil, e
	}
	count = 0
	minimumRadius := math.Min(2, math.Max(.35, 40/math.Sqrt(float64(max(1, info.Hosts)))))
	for rows.Next() {
		if count%4096 == 0 {
			if e = ctx.Err(); e != nil {
				break
			}
		}
		count++
		var x, y, rank float64
		var community int
		if e = rows.Scan(&x, &y, &rank, &community); e != nil {
			break
		}
		importance := 1 - math.Exp(-rank*float64(max(1, info.Hosts))/8)
		radius := math.Min(4, math.Max(minimumRadius*(1+importance), (.00035+.0025*importance)*scale))
		atlasDot(img, x*scale-ox, y*scale-oy, radius, atlasCommunityColor(community))
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	return img, e
}

func AtlasPick(ctx context.Context, path, snapshot string, x, y, radius float64) (*AtlasNeighbor, error) {
	if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) || x < -1 || x > 2 || y < -1 || y > 2 || math.IsNaN(radius) || radius <= 0 || radius > .1 {
		return nil, fmt.Errorf("invalid graph pick")
	}
	db, e := openAtlas(path)
	if e != nil {
		return nil, e
	}
	defer db.Close()
	tx, e := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if _, e = atlasInfoTx(ctx, tx, snapshot); e != nil {
		return nil, e
	}
	var host AtlasNeighbor
	e = tx.QueryRowContext(ctx, `SELECT n.id,n.name,n.x,n.y FROM spatial r JOIN nodes n ON n.id=r.id WHERE r.maxx>=? AND r.maxy>=? AND r.minx<=? AND r.miny<=? AND (n.x-?)*(n.x-?)+(n.y-?)*(n.y-?)<=? ORDER BY (n.x-?)*(n.x-?)+(n.y-?)*(n.y-?),n.rank DESC,n.id LIMIT 1`, x-radius, y-radius, x+radius, y+radius, x, x, y, y, radius*radius, x, x, y, y).Scan(&host.ID, &host.Name, &host.X, &host.Y)
	if e == sql.ErrNoRows {
		return nil, nil
	}
	return &host, e
}

func ForgetAtlas(path string) {
	atlasTiles.Lock()
	defer atlasTiles.Unlock()
	atlasTiles.epochs[path]++
	for key, e := range atlasTiles.items {
		if strings.HasPrefix(key, path+":") {
			v := e.Value.(atlasCachedTile)
			atlasTiles.bytes -= len(v.data)
			atlasTiles.order.Remove(e)
			delete(atlasTiles.items, key)
		}
	}
}

// AtlasEpoch invalidates responses that began before a snapshot was purged.
func AtlasEpoch(path string) uint64 {
	atlasTiles.Lock()
	defer atlasTiles.Unlock()
	return atlasTiles.epochs[path]
}
