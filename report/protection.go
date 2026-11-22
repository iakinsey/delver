package report

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/site"
	"net/url"
	"strconv"
	"strings"
)

type SiteDistance struct {
	Site     string `json:"site"`
	Distance int    `json:"distance"`
}

// ensureSiteHosts upgrades only host metadata. Each batch is replay safe and
// bounded; URL receipts and their historical hashes are left untouched.
func (s *Store) ensureSiteHosts(ctx context.Context) error {
	if _, e := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS site_hosts(host INTEGER PRIMARY KEY,site TEXT NOT NULL);CREATE INDEX IF NOT EXISTS site_hosts_site ON site_hosts(site,host)`); e != nil {
		return e
	}
	var cursor int64
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM progress WHERE key='site_hosts_migration'").Scan(&value)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if value == "complete" {
		return nil
	}
	if value != "" {
		var e error
		cursor, e = strconv.ParseInt(value, 10, 64)
		if e != nil {
			return e
		}
	}
	for {
		type pair struct {
			id   int64
			name string
		}
		rows, e := s.db.QueryContext(ctx, `SELECT h.id,h.name FROM hosts h LEFT JOIN site_hosts s ON s.host=h.id WHERE h.id>? ORDER BY h.id LIMIT 512`, cursor)
		if e != nil {
			return e
		}
		batch := []pair{}
		for rows.Next() {
			var p pair
			if e = rows.Scan(&p.id, &p.name); e != nil {
				rows.Close()
				return e
			}
			batch = append(batch, p)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(batch) == 0 {
			_, e := s.db.ExecContext(ctx, "INSERT OR REPLACE INTO progress(key,value) VALUES('site_hosts_migration','complete')")
			return e
		}
		tx, e := s.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		for _, p := range batch {
			if _, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO site_hosts(host,site) VALUES(?,?)`, p.id, site.Key(p.name)); e != nil {
				break
			}
		}
		if e != nil {
			tx.Rollback()
			return e
		}
		cursor = batch[len(batch)-1].id
		if _, e = tx.ExecContext(ctx, "INSERT OR REPLACE INTO progress(key,value) VALUES('site_hosts_migration',?)", strconv.FormatInt(cursor, 10)); e != nil {
			tx.Rollback()
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
	}
}
func (s *Store) IncomingSites(ctx context.Context, root string, depth int) ([]SiteDistance, int64, bool, error) {
	if depth < 0 {
		return nil, 0, false, fmt.Errorf("negative purge depth")
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, 0, false, e
	}
	defer tx.Rollback()
	var revision int64
	var partial bool
	if e = tx.QueryRowContext(ctx, `SELECT revision,partial FROM meta WHERE id=1`).Scan(&revision, &partial); e != nil {
		return nil, 0, false, e
	}
	// The temporary traversal index bounds each SQL result page. No whole-graph
	// topology cache or generated visual community is involved.
	if _, e = tx.ExecContext(ctx, `CREATE TEMP TABLE purge_walk(site TEXT PRIMARY KEY,distance INTEGER NOT NULL);CREATE INDEX purge_walk_distance ON purge_walk(distance,site);INSERT INTO purge_walk VALUES(?,0)`, site.Key(root)); e != nil {
		return nil, 0, false, e
	}
	for d := 0; d < depth; d++ {
		r, e := tx.ExecContext(ctx, `INSERT OR IGNORE INTO purge_walk(site,distance) SELECT DISTINCT src.site,? FROM purge_walk w JOIN site_hosts dst ON dst.site=w.site JOIN edges e ON e.target=dst.host JOIN site_hosts src ON src.host=e.source WHERE w.distance=?`, d+1, d)
		if e != nil {
			return nil, 0, false, e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return nil, 0, false, e
		}
		if n == 0 {
			break
		}
	}
	rows, e := tx.QueryContext(ctx, `SELECT site,distance FROM purge_walk ORDER BY distance,site`)
	if e != nil {
		return nil, 0, false, e
	}
	out := []SiteDistance{}
	for rows.Next() {
		var v SiteDistance
		if e = rows.Scan(&v.Site, &v.Distance); e != nil {
			rows.Close()
			return nil, 0, false, e
		}
		out = append(out, v)
	}
	e = rows.Err()
	rows.Close()
	return out, revision, partial, e
}
func (s *Store) PurgeSite(ctx context.Context, key string) error {
	return s.batch(ctx, func(tx *sql.Tx) error {
		var known, visited int64
		if e := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(h.known),0),COALESCE(SUM(h.visited),0) FROM hosts h JOIN site_hosts s ON s.host=h.id WHERE s.site=?`, key).Scan(&known, &visited); e != nil {
			return e
		}
		for _, q := range []string{`DELETE FROM urls WHERE host IN(SELECT host FROM site_hosts WHERE site=?)`, `DELETE FROM edges WHERE source IN(SELECT host FROM site_hosts WHERE site=?) OR target IN(SELECT host FROM site_hosts WHERE site=?)`, `DELETE FROM hosts WHERE id IN(SELECT host FROM site_hosts WHERE site=?)`, `DELETE FROM site_hosts WHERE site=?`} {
			args := []interface{}{key}
			if q[12:17] == "edges" {
				args = append(args, key)
			}
			if _, e := tx.ExecContext(ctx, q, args...); e != nil {
				return e
			}
		}
		_, e := tx.ExecContext(ctx, `UPDATE meta SET known=known-?,visited=visited-?,host_count=(SELECT count(*) FROM hosts),edge_count=(SELECT count(*) FROM edges),revision=revision+1 WHERE id=1`, known, visited)
		return e
	})
}

func (s *Store) PurgeMatchingHosts(ctx context.Context, match func(string) bool) error {
	var after int64
	for {
		rows, e := s.db.QueryContext(ctx, `SELECT id,name FROM hosts WHERE id>? ORDER BY id LIMIT 256`, after)
		if e != nil {
			return e
		}
		ids := []int64{}
		n := 0
		for rows.Next() {
			var id int64
			var name string
			if e = rows.Scan(&id, &name); e != nil {
				rows.Close()
				return e
			}
			n++
			after = id
			if match(name) {
				ids = append(ids, id)
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(ids) > 0 {
			if e = s.batch(ctx, func(tx *sql.Tx) error {
				var known, visited int64
				for _, id := range ids {
					var k, v int64
					if e := tx.QueryRowContext(ctx, `SELECT known,visited FROM hosts WHERE id=?`, id).Scan(&k, &v); e != nil {
						return e
					}
					known += k
					visited += v
					for _, q := range []string{`DELETE FROM urls WHERE host=?`, `DELETE FROM edges WHERE source=? OR target=?`, `DELETE FROM site_hosts WHERE host=?`, `DELETE FROM hosts WHERE id=?`} {
						args := []interface{}{id}
						if strings.Contains(q, " OR ") {
							args = append(args, id)
						}
						if _, e := tx.ExecContext(ctx, q, args...); e != nil {
							return e
						}
					}
				}
				_, e := tx.ExecContext(ctx, `UPDATE meta SET known=known-?,visited=visited-?,host_count=(SELECT count(*) FROM hosts),edge_count=(SELECT count(*) FROM edges),revision=revision+1 WHERE id=1`, known, visited)
				return e
			}); e != nil {
				return e
			}
		}
		if n < 256 {
			return nil
		}
	}
}
func (s *Store) PurgeURL(ctx context.Context, raw string) error {
	uri, _, e := Normalize(raw)
	if e != nil {
		return e
	}
	key := sha256.Sum256([]byte(uri))
	return s.batch(ctx, func(tx *sql.Tx) error {
		var host int64
		var visited int
		e := tx.QueryRowContext(ctx, `SELECT host,responded FROM urls WHERE key=?`, key[:]).Scan(&host, &visited)
		if e == sql.ErrNoRows {
			return nil
		}
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM urls WHERE key=?`, key[:]); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE hosts SET known=known-1,visited=visited-? WHERE id=?`, visited, host); e != nil {
			return e
		}

		var known int64
		if e = tx.QueryRowContext(ctx, `SELECT known FROM hosts WHERE id=?`, host).Scan(&known); e != nil {
			return e
		}
		if known == 0 {
			if _, e = tx.ExecContext(ctx, `DELETE FROM edges WHERE source=? OR target=?`, host, host); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `DELETE FROM site_hosts WHERE host=?`, host); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `DELETE FROM hosts WHERE id=?`, host); e != nil {
				return e
			}
		}
		_, e = tx.ExecContext(ctx, `UPDATE meta SET known=known-1,visited=visited-?,host_count=(SELECT count(*) FROM hosts),edge_count=(SELECT count(*) FROM edges),revision=revision+1 WHERE id=1`, visited)
		return e
	})
}

type MatchingCounts struct {
	URLs    int64 `json:"urls"`
	Hosts   int64 `json:"hosts"`
	Partial bool  `json:"partial"`
}

func (s *Store) MatchingCounts(ctx context.Context, entries []protection.Entry) (MatchingCounts, error) {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return MatchingCounts{}, e
	}
	defer tx.Rollback()
	seen := map[string]bool{}
	matcher := protection.NewMatcher(entries, nil)
	out := MatchingCounts{}
	rows, e := tx.QueryContext(ctx, `SELECT name,known FROM hosts`)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var name string
		var known int64
		if e = rows.Scan(&name, &known); e != nil {
			rows.Close()
			return out, e
		}
		if matcher.MatchesDomain(name) {
			out.URLs += known
			out.Hosts++
			seen[name] = true
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for _, entry := range entries {
		if entry.Domain {
			continue
		}
		u, e := url.Parse(entry.Value)
		if e != nil {
			return out, e
		}
		if matcher.MatchesDomain(u.Hostname()) {
			continue
		}
		uri, _, e := Normalize(entry.Value)
		if e != nil {
			return out, e
		}
		key := sha256.Sum256([]byte(uri))
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM urls WHERE key=?`, key[:]).Scan(&n); e != nil {
			return out, e
		}
		out.URLs += int64(n)
		if n > 0 && !seen[u.Hostname()] {
			seen[u.Hostname()] = true
			out.Hosts++
		}
	}
	e = tx.QueryRowContext(ctx, `SELECT partial FROM meta WHERE id=1`).Scan(&out.Partial)
	return out, e
}

// VerifyPurged checks named hosts and URL receipts after physical cleanup. WAL
// truncation removes accessible old frames; this is not forensic device erasure.
func (s *Store) VerifyPurged(ctx context.Context, entries []protection.Entry, sites []protection.Target) error {
	matcher := protection.NewMatcher(entries, sites)
	var after int64
	for {
		rows, e := s.db.QueryContext(ctx, `SELECT id,name FROM hosts WHERE id>? ORDER BY id LIMIT 256`, after)
		if e != nil {
			return e
		}
		n := 0
		for rows.Next() {
			var name string
			if e = rows.Scan(&after, &name); e != nil {
				rows.Close()
				return e
			}
			n++
			if matcher.MatchesDomain(name) {
				rows.Close()
				return fmt.Errorf("matching report host remains")
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if n < 256 {
			break
		}
	}
	for _, entry := range entries {
		if entry.Domain {
			continue
		}
		uri, _, e := Normalize(entry.Value)
		if e != nil {
			return e
		}
		key := sha256.Sum256([]byte(uri))
		var n int
		if e = s.db.QueryRowContext(ctx, `SELECT count(*) FROM urls WHERE key=?`, key[:]).Scan(&n); e != nil {
			return e
		}
		if n > 0 {
			return fmt.Errorf("matching report URL remains")
		}
	}
	s.write.Lock()
	defer s.write.Unlock()
	var busy, frames, checkpointed int
	if e := s.db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &frames, &checkpointed); e != nil {
		return e
	}
	if busy != 0 {
		return fmt.Errorf("report checkpoint busy")
	}
	return nil
}
