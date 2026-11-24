package report

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/repetition"
)

func (s *Store) ensureRepeatedPages() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS repeated_meta(id INTEGER PRIMARY KEY CHECK(id=1),revision INTEGER NOT NULL,epoch TEXT NOT NULL);
 INSERT OR IGNORE INTO repeated_meta VALUES(1,0,'');
 CREATE TABLE IF NOT EXISTS repeated_families(id TEXT PRIMARY KEY,epoch TEXT NOT NULL,confirmed INTEGER NOT NULL,exempt INTEGER NOT NULL,representative TEXT NOT NULL,payload BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS repeated_sites(site TEXT PRIMARY KEY,family TEXT NOT NULL,epoch TEXT NOT NULL,uri TEXT NOT NULL,payload BLOB NOT NULL);
 CREATE INDEX IF NOT EXISTS repeated_sites_family ON repeated_sites(family,site);`)
	return err
}

// Projection records originate in the frontier's durable outbox. The applied
// revision and rows commit together; a replay after either process crashes is safe.
func (s *Store) RepeatedPages(updates []repetition.Update) error {
	ctx := context.Background()
	uris := []string{}
	filtered := make([]repetition.Update, 0, len(updates))
	for _, u := range updates {
		sources := []string{}
		if u.Host != nil && u.Host.URI != "" {
			sources = append(sources, u.Host.URI)
		}
		for _, h := range u.Hosts {
			if h.URI != "" {
				sources = append(sources, h.URI)
			}
		}
		for _, f := range u.Families {
			if f.Representative != "" {
				sources = append(sources, f.Representative)
			}
		}
		blocked := false
		for _, raw := range sources {
			if err := s.policy.Check(raw); errors.Is(err, protection.ErrBlocked) {
				blocked = true
			} else if err != nil {
				return err
			}
		}
		if !blocked {
			filtered = append(filtered, u)
			uris = append(uris, sources...)
		} else {
			filtered = append(filtered, repetition.Update{Revision: u.Revision, Epoch: u.Epoch})
		}
	}
	return s.policy.GuardMany(uris, func() error {
		return s.batch(ctx, func(tx *sql.Tx) error {
			var revision int64
			if err := tx.QueryRow(`SELECT revision FROM repeated_meta WHERE id=1`).Scan(&revision); err != nil {
				return err
			}
			for _, u := range filtered {
				if u.Revision <= revision {
					continue
				}
				for _, f := range u.Families {
					if !f.Confirmed && f.Sites == 0 && f.Representative == "" {
						if _, err := tx.Exec(`DELETE FROM repeated_families WHERE id=?`, f.ID); err != nil {
							return err
						}
						continue
					}
					data, err := json.Marshal(f)
					if err != nil {
						return err
					}
					if _, err = tx.Exec(`INSERT OR REPLACE INTO repeated_families VALUES(?,?,?,?,?,?)`, f.ID, f.Epoch, f.Confirmed, f.Exempt, f.Representative, data); err != nil {
						return err
					}
				}
				hosts := u.Hosts
				if u.Host != nil {
					hosts = append(append([]repetition.Host{}, hosts...), *u.Host)
				}
				for _, h := range hosts {
					data, err := json.Marshal(h)
					if err != nil {
						return err
					}
					key := h.Host
					if key == "" {
						key = h.Site
					}
					if _, err = tx.Exec(`INSERT OR REPLACE INTO repeated_sites VALUES(?,?,?,?,?)`, key, h.Family, h.Epoch, h.URI, data); err != nil {
						return err
					}
				}
				if _, err := tx.Exec(`UPDATE repeated_meta SET revision=?,epoch=? WHERE id=1`, u.Revision, u.Epoch); err != nil {
					return err
				}
				revision = u.Revision
			}
			return nil
		})
	})
}

func (s *Store) purgeRepeated(ctx context.Context, match func(string) bool) error {
	for _, table := range []string{"repeated_families", "repeated_sites"} {
		after := ""
		key := "id"
		source := "representative"
		if table == "repeated_sites" {
			key = "site"
			source = "uri"
		}
		for {
			type row struct{ key, uri, family string }
			batch := []row{}
			extra := "id"
			if table == "repeated_sites" {
				extra = "family"
			}
			rows, err := s.db.QueryContext(ctx, "SELECT "+key+","+source+","+extra+" FROM "+table+" WHERE "+key+">? ORDER BY "+key+" LIMIT 128", after)
			if err != nil {
				return err
			}
			n := 0
			for rows.Next() {
				var r row
				if err = rows.Scan(&r.key, &r.uri, &r.family); err != nil {
					break
				}
				n++
				after = r.key
				if match(r.uri) {
					batch = append(batch, r)
				}
			}
			if err == nil {
				err = rows.Err()
			}
			rows.Close()
			if err != nil {
				return err
			}
			if len(batch) > 0 {
				if err = s.batch(ctx, func(tx *sql.Tx) error {
					for _, r := range batch {
						if _, err := tx.ExecContext(ctx, `DELETE FROM repeated_families WHERE id=?`, r.family); err != nil {
							return err
						}
						if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+key+"=?", r.key); err != nil {
							return err
						}
					}
					return nil
				}); err != nil {
					return err
				}
			}
			if n < 128 {
				break
			}
		}
	}
	return nil
}

func (s *Store) RepeatedRevision(ctx context.Context) (int64, error) {
	var revision int64
	err := s.db.QueryRowContext(ctx, `SELECT revision FROM repeated_meta WHERE id=1`).Scan(&revision)
	return revision, err
}
