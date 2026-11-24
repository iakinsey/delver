package report

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type atlasMember struct {
	group    int64
	original atlasVertex
}
type atlasFold struct {
	members      []atlasMember
	families     map[int64]string
	internal     map[int64]int
	multiplicity map[[2]int64]int
}

// Folding consumes the same consistent source transaction as topology reads.
// Raw input and the additional membership/index allocations are budgeted first.
func foldAtlas(ctx context.Context, tx *sql.Tx, nodes []atlasVertex, edges []atlasLink, check func(int64) error) ([]atlasVertex, []atlasLink, *atlasFold, error) {
	if err := check(int64(len(nodes))*480 + int64(len(edges))*96); err != nil {
		return nil, nil, nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT s.site,s.family FROM repeated_sites s JOIN repeated_families f ON f.id=s.family JOIN repeated_meta m ON m.id=1 WHERE f.confirmed=1 AND f.exempt=0 AND s.epoch=m.epoch AND f.epoch=m.epoch AND m.epoch<>''`)
	if err != nil {
		return nil, nil, nil, err
	}
	wanted := make(map[string]bool, len(nodes))
	for _, v := range nodes {
		wanted[v.name] = true
	}
	families := map[string]string{}
	for rows.Next() {
		var site, family string
		if err = rows.Scan(&site, &family); err != nil {
			break
		}
		if wanted[site] {
			families[site] = family
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, nil, nil, err
	}
	fold := &atlasFold{families: map[int64]string{}, internal: map[int64]int{}, multiplicity: map[[2]int64]int{}}
	if len(families) == 0 {
		return nodes, edges, fold, nil
	}
	grouped := []atlasVertex{}
	remap := make([]int, len(nodes))
	groups := map[string]int{}
	for i, v := range nodes {
		if i%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, nil, err
			}
		}
		family := families[v.name]
		key := fmt.Sprintf("host:%d", v.id)
		if family != "" {
			key = "family:" + family
		}
		group, exists := groups[key]
		if !exists {
			group = len(grouped)
			groups[key] = group
			copy := v
			copy.in = nil
			copy.out = nil
			if family != "" {
				copy.name = "Family " + family[:12]
				fold.families[copy.id] = family
			}
			copy.known = 0
			copy.visited = 0
			grouped = append(grouped, copy)
		}
		grouped[group].known += v.known
		grouped[group].visited += v.visited
		remap[i] = group
		if family != "" {
			fold.members = append(fold.members, atlasMember{grouped[group].id, v})
		}
	}
	links := []atlasLink{}
	for _, e := range edges {
		a, b := remap[e.a], remap[e.b]
		aid, bid := grouped[a].id, grouped[b].id
		if a == b {
			fold.internal[aid]++
			continue
		}
		key := [2]int64{aid, bid}
		fold.multiplicity[key]++
		if fold.multiplicity[key] > 1 {
			continue
		}
		grouped[a].out = append(grouped[a].out, b)
		grouped[b].in = append(grouped[b].in, a)
		links = append(links, atlasLink{a, b})
	}
	return grouped, links, fold, nil
}

func writeAtlasFold(ctx context.Context, tx *sql.Tx, fold *atlasFold, nodes []atlasVertex) error {
	if fold == nil {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE family_nodes(node INTEGER PRIMARY KEY,family TEXT NOT NULL,internal INTEGER NOT NULL);CREATE TABLE members(id INTEGER PRIMARY KEY,node INTEGER NOT NULL,name TEXT NOT NULL,known INTEGER NOT NULL,visited INTEGER NOT NULL,incoming INTEGER NOT NULL,outgoing INTEGER NOT NULL);CREATE INDEX members_node ON members(node,name);CREATE INDEX members_name ON members(name);CREATE TABLE edge_multiplicity(source INTEGER,target INTEGER,links INTEGER,PRIMARY KEY(source,target));`); err != nil {
		return err
	}
	present := map[int64]bool{}
	for _, v := range nodes {
		present[v.id] = true
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO members VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, m := range fold.members {
		if !present[m.group] {
			continue
		}
		v := m.original
		if _, err = stmt.ExecContext(ctx, v.id, m.group, v.name, v.known, v.visited, len(v.in), len(v.out)); err != nil {
			return err
		}
	}
	for id, family := range fold.families {
		if present[id] {
			if _, err = tx.ExecContext(ctx, `INSERT INTO family_nodes VALUES(?,?,?)`, id, family, fold.internal[id]); err != nil {
				return err
			}
		}
	}
	for pair, count := range fold.multiplicity {
		if count > 1 && present[pair[0]] && present[pair[1]] {
			if _, err = tx.ExecContext(ctx, `INSERT INTO edge_multiplicity VALUES(?,?,?)`, pair[0], pair[1], count); err != nil {
				return err
			}
		}
	}
	return nil
}

type AtlasMember struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Known    int64  `json:"known"`
	Visited  int64  `json:"visited"`
	Incoming int    `json:"incoming"`
	Outgoing int    `json:"outgoing"`
}
type AtlasMembersPage struct {
	Members  []AtlasMember `json:"members"`
	More     bool          `json:"more"`
	Total    int           `json:"total"`
	Internal int           `json:"internal"`
	Family   string        `json:"family"`
}

func AtlasMembers(ctx context.Context, path, snapshot string, id int64, query string, offset int) (out AtlasMembersPage, err error) {
	if id <= 0 || offset < 0 || offset > 2000000 || len(query) > 253 {
		return out, fmt.Errorf("invalid member query")
	}
	db, err := openAtlas(path)
	if err != nil {
		return out, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	info, err := atlasInfoTx(ctx, tx, snapshot)
	if err != nil {
		return out, err
	}
	out.Members = []AtlasMember{}
	if !info.Options.FoldRepeatedPages {
		return out, nil
	}
	err = tx.QueryRowContext(ctx, `SELECT family,internal FROM family_nodes WHERE node=?`, id).Scan(&out.Family, &out.Internal)
	if err == sql.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM members WHERE node=?`, id).Scan(&out.Total); err != nil {
		return out, err
	}
	query = strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(query)
	rows, err := tx.QueryContext(ctx, `SELECT id,name,known,visited,incoming,outgoing FROM members WHERE node=? AND name LIKE ? ESCAPE '\' ORDER BY name LIMIT 101 OFFSET ?`, id, "%"+query+"%", offset)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var m AtlasMember
		if err = rows.Scan(&m.ID, &m.Name, &m.Known, &m.Visited, &m.Incoming, &m.Outgoing); err != nil {
			return out, err
		}
		out.Members = append(out.Members, m)
	}
	err = rows.Err()
	out.More = len(out.Members) > 100
	if out.More {
		out.Members = out.Members[:100]
	}
	return out, err
}
