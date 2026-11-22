package report

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

func openAtlas(path string) (*sql.DB, error) {
	return sql.Open("sqlite3", "file:"+path+"?mode=ro&_busy_timeout=1000&_cache_size=-4096")
}
func ReadAtlasInfo(ctx context.Context, path string) (info AtlasInfo, err error) {
	db, err := openAtlas(path)
	if err != nil {
		return
	}
	defer db.Close()
	var b string
	err = db.QueryRowContext(ctx, "SELECT value FROM meta").Scan(&b)
	if err == nil {
		err = json.Unmarshal([]byte(b), &info)
	}
	return
}

type AtlasViewport struct {
	CountsOnly   bool      `json:"counts_only"`
	Snapshot     string    `json:"snapshot"`
	SelectedHost int64     `json:"selected_host"`
	Bounds       []float64 `json:"bounds"`
	NodeLimit    int       `json:"node_limit"`
	EdgeLimit    int       `json:"edge_limit"`
}
type AtlasNode struct {
	Pinned    bool    `json:"pinned,omitempty"`
	ID        string  `json:"id"`
	HostID    int64   `json:"host_id"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Members   int     `json:"members"`
	Community int     `json:"community"`
	Authority float64 `json:"authority"`
	Internal  int     `json:"internal"`
}
type AtlasFrame struct {
	Detailed   bool        `json:"detailed"`
	Rasterized bool        `json:"rasterized"`
	Focus      []GraphEdge `json:"focus,omitempty"`
	Nodes      []AtlasNode `json:"nodes"`
	Edges      []GraphEdge `json:"edges"`
	Grouped    bool        `json:"grouped"`
	Level      int         `json:"level"`
	Snapshot   string      `json:"snapshot"`
	Hosts      int         `json:"hosts"`
	Links      int         `json:"links"`
}

func AtlasView(ctx context.Context, path string, o AtlasViewport) (frame AtlasFrame, err error) {
	frame.Nodes = []AtlasNode{}
	frame.Edges = []GraphEdge{}
	if o.NodeLimit < 50 || o.NodeLimit > 5000 || o.EdgeLimit < 100 || o.EdgeLimit > 20000 {
		return frame, fmt.Errorf("viewport limits: 50–5000 nodes, 100–20000 edges")
	}
	if len(o.Bounds) != 4 {
		return frame, fmt.Errorf("four viewport bounds required")
	}
	b := o.Bounds
	for _, v := range b {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < -1 || v > 2 {
			return frame, fmt.Errorf("invalid viewport bounds")
		}
	}
	if b[0] >= b[2] || b[1] >= b[3] {
		return frame, fmt.Errorf("empty viewport")
	}
	db, err := openAtlas(path)
	if err != nil {
		return frame, err
	}
	defer db.Close()
	// Pin the artifact inode before reading its identity. Regeneration atomically
	// replaces the file; identity and data must come from the same connection.
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return frame, err
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRowContext(ctx, "SELECT value FROM meta").Scan(&raw); err != nil {
		return frame, err
	}
	var info AtlasInfo
	if err = json.Unmarshal([]byte(raw), &info); err != nil {
		return frame, err
	}
	if o.Snapshot != info.ID {
		return frame, fmt.Errorf("snapshot changed; reload graph status")
	}
	frame.Snapshot = info.ID

	if info.Format != 2 {
		return frame, fmt.Errorf("regenerate graph snapshot to update its layout")
	}
	frame.Rasterized = true
	visible := `SELECT n.* FROM spatial r JOIN nodes n ON n.id=r.id WHERE r.maxx>=? AND r.maxy>=? AND r.minx<=? AND r.miny<=?`
	args := []interface{}{b[0], b[1], b[2], b[3]}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM ("+visible+")", args...).Scan(&frame.Hosts); err != nil {
		return frame, err
	}
	if o.CountsOnly {
		return frame, nil
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM edge_spatial WHERE maxx>=? AND maxy>=? AND minx<=? AND miny<=?`, args...).Scan(&frame.Links); err != nil {
		return frame, err
	}
	frame.Detailed = frame.Hosts <= o.NodeLimit && frame.Links <= o.EdgeLimit
	cte := "WITH chosen AS (" + visible + " ORDER BY rank DESC,id LIMIT ?) "
	params := append(append([]interface{}{}, args...), o.NodeLimit)
	rows, e := tx.QueryContext(ctx, cte+"SELECT id,x,y,community,rank FROM chosen ORDER BY rank DESC,id", params...)
	if e != nil {
		return frame, e
	}
	for rows.Next() {
		var v AtlasNode
		if e = rows.Scan(&v.HostID, &v.X, &v.Y, &v.Community, &v.Authority); e != nil {
			break
		}
		v.ID = fmt.Sprintf("h:%d", v.HostID)
		v.Members = 1
		frame.Nodes = append(frame.Nodes, v)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return frame, e
	}
	rows, e = tx.QueryContext(ctx, cte+"SELECT e.source,e.target FROM chosen a JOIN edges e ON e.source=a.id JOIN chosen b ON b.id=e.target ORDER BY e.source,e.target LIMIT ?", append(params, o.EdgeLimit)...)
	if e != nil {
		return frame, e
	}
	for rows.Next() {
		var a, b int64
		if e = rows.Scan(&a, &b); e != nil {
			break
		}
		frame.Edges = append(frame.Edges, GraphEdge{Source: fmt.Sprintf("h:%d", a), Target: fmt.Sprintf("h:%d", b), Links: 1})
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return frame, e
	}
	if o.SelectedHost > 0 {
		found := false
		for _, v := range frame.Nodes {
			if v.HostID == o.SelectedHost {
				found = true
				break
			}
		}
		if !found {
			var v AtlasNode
			if err = tx.QueryRowContext(ctx, "SELECT id,x,y,community,rank FROM nodes WHERE id=?", o.SelectedHost).Scan(&v.HostID, &v.X, &v.Y, &v.Community, &v.Authority); err != nil {
				return frame, err
			}
			v.ID = fmt.Sprintf("h:%d", v.HostID)
			v.Pinned = true
			if len(frame.Nodes) == o.NodeLimit {
				frame.Nodes = frame.Nodes[:len(frame.Nodes)-1]
			}
			frame.Nodes = append(frame.Nodes, v)
		}
	}
	return frame, nil

}

type AtlasHost struct {
	Host
	Authority float64 `json:"authority"`
	Community int     `json:"community"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Incoming  int     `json:"incoming"`
	Outgoing  int     `json:"outgoing"`
	Bridges   int     `json:"bridges"`
}
type AtlasNeighbor struct {
	ID   int64   `json:"id"`
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}
type AtlasInspection struct {
	Host      AtlasHost       `json:"host"`
	Neighbors []AtlasNeighbor `json:"neighbors"`
	More      bool            `json:"more"`
	Direction string          `json:"direction"`
}

func AtlasInspect(ctx context.Context, path, snapshot string, id int64, direction string, offset int) (out AtlasInspection, err error) {
	if id <= 0 || offset < 0 || offset > 10000000 || (direction != "incoming" && direction != "outgoing") {
		return out, fmt.Errorf("invalid host inspection")
	}
	db, err := openAtlas(path)
	if err != nil {
		return
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRowContext(ctx, "SELECT value FROM meta").Scan(&raw); err != nil {
		return
	}
	var info AtlasInfo
	if err = json.Unmarshal([]byte(raw), &info); err != nil {
		return
	}
	if info.ID != snapshot {
		return out, fmt.Errorf("snapshot changed; reload graph status")
	}
	h := &out.Host
	err = tx.QueryRowContext(ctx, "SELECT id,name,known,visited,rank,community,x,y,incoming,outgoing,bridges FROM nodes WHERE id=?", id).Scan(&h.ID, &h.Name, &h.Known, &h.Visited, &h.Authority, &h.Community, &h.X, &h.Y, &h.Incoming, &h.Outgoing, &h.Bridges)
	if err != nil {
		return
	}
	from, to := "source", "target"
	if direction == "incoming" {
		from, to = to, from
	}
	rows, err := tx.QueryContext(ctx, "SELECT n.id,n.name,n.x,n.y FROM edges e JOIN nodes n ON n.id=e."+to+" WHERE e."+from+"=? ORDER BY n.id LIMIT 201 OFFSET ?", id, offset)
	if err != nil {
		return
	}
	defer rows.Close()
	out.Neighbors = []AtlasNeighbor{}
	out.Direction = direction
	for rows.Next() {
		var v AtlasNeighbor
		if err = rows.Scan(&v.ID, &v.Name, &v.X, &v.Y); err != nil {
			return
		}
		out.Neighbors = append(out.Neighbors, v)
	}
	err = rows.Err()
	out.More = len(out.Neighbors) > 200
	if out.More {
		out.Neighbors = out.Neighbors[:200]
	}
	return
}
func AtlasSearch(ctx context.Context, path, query string, snapshot ...string) (out []AtlasNeighbor, err error) {
	if len(query) > 253 {
		return nil, fmt.Errorf("query too long")
	}
	db, err := openAtlas(path)
	if err != nil {
		return
	}
	defer db.Close()
	tx, e := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if len(snapshot) > 0 && snapshot[0] != "" {
		if _, e = atlasInfoTx(ctx, tx, snapshot[0]); e != nil {
			return nil, e
		}
	}
	query = strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(query)
	rows, err := tx.QueryContext(ctx, `SELECT id,name,x,y FROM nodes WHERE name LIKE ? ESCAPE '\' ORDER BY incoming+outgoing DESC,id LIMIT 20`, "%"+query+"%")
	if err != nil {
		return
	}
	defer rows.Close()
	out = []AtlasNeighbor{}
	for rows.Next() {
		var v AtlasNeighbor
		if err = rows.Scan(&v.ID, &v.Name, &v.X, &v.Y); err != nil {
			return
		}
		out = append(out, v)
	}
	err = rows.Err()
	return
}
