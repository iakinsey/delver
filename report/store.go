// Package report records run-scoped URL receipts and a directed graph of hosts.
// It is independent of traversal, frontier retention, and result sinks.
package report

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/site"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/net/idna"
)

type URL struct{ URI, Origin string }
type Writer interface {
	Discover(context.Context, []URL) error
	Response(context.Context, string, int) error
}
type Store struct {
	policy    *protection.Policy
	db        *sql.DB
	write     *sync.Mutex
	path      string
	writer    *ledgerWriter
	closeOnce sync.Once
	closeErr  error
}

// Handles for live workers, historical import and API reads share one writer
// mutex per ledger. SQLite's busy timeout alone does not guarantee admission
// between consecutive import batches.
type ledgerWriter struct {
	sync.Mutex
	waiting waitingWrites
	refs    int
}

var ledgerWriters = struct {
	sync.Mutex
	values map[string]*ledgerWriter
}{values: map[string]*ledgerWriter{}}

func retainWriter(path string) *ledgerWriter {
	ledgerWriters.Lock()
	defer ledgerWriters.Unlock()
	w := ledgerWriters.values[path]
	if w == nil {
		w = &ledgerWriter{}
		ledgerWriters.values[path] = w
	}
	w.refs++
	return w
}
func releaseWriter(path string, w *ledgerWriter) {
	ledgerWriters.Lock()
	defer ledgerWriters.Unlock()
	w.refs--
	if w.refs == 0 {
		delete(ledgerWriters.values, path)
	}
}

type Counts struct {
	Known      int64 `json:"known"`
	Visited    int64 `json:"visited"`
	NotVisited int64 `json:"not_visited"`
	Hosts      int64 `json:"hosts"`
	Edges      int64 `json:"edges"`
	Revision   int64 `json:"revision"`
	Partial    bool  `json:"partial"`
}
type Host struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Known       int64  `json:"known"`
	Visited     int64  `json:"visited"`
	Connections int64  `json:"connections"`
	Cursor      string `json:"cursor,omitempty"`
}

// Count neighbors, not URLs or directed edges: a reciprocal link contributes
// one connected host. Both arms use the existing source/target indexes.
const hostConnections = `(SELECT COUNT(*) FROM (
 SELECT target AS peer FROM edges WHERE source=h.id
 UNION SELECT source FROM edges WHERE target=h.id
))`

func hostCursor(h Host) string { return fmt.Sprintf("connections:%d:%s", h.Connections, h.Name) }
func (s *Store) searchCursor(ctx context.Context, after string) (int64, string, error) {
	if len(after) > 4096 {
		return 0, "", fmt.Errorf("host cursor too long")
	}
	if strings.HasPrefix(after, "connections:") {
		fields := strings.SplitN(strings.TrimPrefix(after, "connections:"), ":", 2)
		if len(fields) != 2 || fields[1] == "" {
			return 0, "", fmt.Errorf("invalid host cursor")
		}
		count, e := strconv.ParseInt(fields[0], 10, 64)
		if e != nil || count < 0 {
			return 0, "", fmt.Errorf("invalid host cursor")
		}
		return count, fields[1], nil
	}
	// Retain the hostname cursor accepted by older clients.
	var count int64
	e := s.db.QueryRowContext(ctx, "SELECT "+hostConnections+" FROM hosts h WHERE h.name=?", after).Scan(&count)
	if e == sql.ErrNoRows {
		return 0, "", fmt.Errorf("invalid host cursor")
	}
	return count, after, e
}

func Normalize(raw string) (string, string, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.User != nil {
		return "", "", fmt.Errorf("invalid report URL")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", fmt.Errorf("invalid report URL scheme")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	} else {
		host, e = idna.Lookup.ToASCII(host)
		if e != nil {
			return "", "", e
		}
	}
	u.Host = host
	if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	}
	original, _ := url.Parse(raw)
	p := original.Port()
	if p != "" && !((u.Scheme == "http" && p == "80") || (u.Scheme == "https" && p == "443")) {
		u.Host = net.JoinHostPort(host, p)
	}
	u.Fragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), host, nil
}
func Open(path string, partial bool) (*Store, error) {
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	writer := retainWriter(path)
	success := false
	defer func() {
		if !success {
			releaseWriter(path, writer)
		}
	}()
	writer.Lock()
	defer writer.Unlock()
	u := url.URL{Scheme: "file", Path: path}
	db, e := sql.Open("sqlite3", u.String()+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=FULL&_cache_size=-2048&_foreign_keys=on&_secure_delete=on")
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(1)
	s := &Store{db: db, path: path, writer: writer, write: &writer.Mutex}
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		db.Close()
		return nil, err
	}
	if version == 1 {
		if partial {
			_, e = db.Exec("UPDATE meta SET partial=1 WHERE id=1")
		}
		if e != nil {
			db.Close()
			return nil, e
		}
		if e = s.ensureSiteHosts(context.Background()); e != nil {
			db.Close()
			return nil, e
		}
		success = true
		return s, nil
	}
	_, e = db.Exec(`CREATE TABLE IF NOT EXISTS meta (id INTEGER PRIMARY KEY CHECK(id=1), known INTEGER NOT NULL DEFAULT 0, visited INTEGER NOT NULL DEFAULT 0, revision INTEGER NOT NULL DEFAULT 0, partial INTEGER NOT NULL DEFAULT 0, backfilled INTEGER NOT NULL DEFAULT 0, host_count INTEGER NOT NULL DEFAULT 0, edge_count INTEGER NOT NULL DEFAULT 0);
 INSERT OR IGNORE INTO meta(id,partial) VALUES(1,0);
 CREATE TABLE IF NOT EXISTS progress(key TEXT PRIMARY KEY,value TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS hosts (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, known INTEGER NOT NULL DEFAULT 0, visited INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS urls (key BLOB PRIMARY KEY, host INTEGER NOT NULL REFERENCES hosts(id), responded INTEGER NOT NULL DEFAULT 0) WITHOUT ROWID;
 CREATE TABLE IF NOT EXISTS edges (source INTEGER NOT NULL REFERENCES hosts(id), target INTEGER NOT NULL REFERENCES hosts(id), PRIMARY KEY(source,target)) WITHOUT ROWID;
 CREATE INDEX IF NOT EXISTS incoming ON edges(target,source);
 CREATE TRIGGER IF NOT EXISTS new_url AFTER INSERT ON urls BEGIN UPDATE meta SET known=known+1 WHERE id=1; UPDATE hosts SET known=known+1 WHERE id=NEW.host; END;
 CREATE TRIGGER IF NOT EXISTS received AFTER UPDATE OF responded ON urls WHEN OLD.responded=0 AND NEW.responded=1 BEGIN UPDATE meta SET visited=visited+1 WHERE id=1; UPDATE hosts SET visited=visited+1 WHERE id=NEW.host; END;
 CREATE TRIGGER IF NOT EXISTS new_host AFTER INSERT ON hosts BEGIN UPDATE meta SET revision=revision+1,host_count=host_count+1 WHERE id=1; END;
 CREATE TRIGGER IF NOT EXISTS new_edge AFTER INSERT ON edges BEGIN UPDATE meta SET revision=revision+1,edge_count=edge_count+1 WHERE id=1; END;
 PRAGMA user_version=1;`)
	if e != nil {
		db.Close()
		return nil, e
	}
	if partial {
		_, e = db.Exec("UPDATE meta SET partial=1 WHERE id=1")
	}
	if e != nil {
		db.Close()
		return nil, e
	}
	if e = s.ensureSiteHosts(context.Background()); e != nil {
		db.Close()
		return nil, e
	}
	success = true
	return s, nil
}
func (s *Store) Close() error {
	s.closeOnce.Do(func() { s.closeErr = s.db.Close(); releaseWriter(s.path, s.writer) })
	return s.closeErr
}
func hostID(ctx context.Context, tx *sql.Tx, name string) (int64, error) {
	if _, e := tx.ExecContext(ctx, "INSERT OR IGNORE INTO hosts(name) VALUES(?)", name); e != nil {
		return 0, e
	}
	var id int64
	e := tx.QueryRowContext(ctx, "SELECT id FROM hosts WHERE name=?", name).Scan(&id)
	if e == nil {
		_, e = tx.ExecContext(ctx, "INSERT OR IGNORE INTO site_hosts(host,site) VALUES(?,?)", id, site.Key(name))
	}
	return id, e
}
func recordURL(ctx context.Context, tx *sql.Tx, raw string, received bool) (int64, error) {
	uri, name, e := Normalize(raw)
	if e != nil {
		return 0, e
	}
	id, e := hostID(ctx, tx, name)
	if e != nil {
		return 0, e
	}
	hash := sha256.Sum256([]byte(uri))
	if _, e = tx.ExecContext(ctx, "INSERT OR IGNORE INTO urls(key,host) VALUES(?,?)", hash[:], id); e != nil {
		return 0, e
	}
	if received {
		_, e = tx.ExecContext(ctx, "UPDATE urls SET responded=1 WHERE key=? AND responded=0", hash[:])
	}
	return id, e
}
func (s *Store) SetProtection(p *protection.Policy) { s.policy = p }
func (s *Store) Discover(ctx context.Context, urls []URL) error {
	if e := s.policy.HealthError(); e != nil {
		return e
	}
	filtered := make([]URL, 0, len(urls))
	for _, u := range urls {
		blocked := false
		for _, raw := range []string{u.URI, u.Origin} {
			if raw == "" {
				continue
			}
			if e := s.policy.Check(raw); e != nil {
				if errors.Is(e, protection.ErrBlocked) {
					blocked = true
				} else {
					return e
				}
			}
		}
		if !blocked {
			filtered = append(filtered, u)
		}
	}
	urls = filtered

	for start := 0; start < len(urls); start += 2048 {
		end := start + 2048
		if end > len(urls) {
			end = len(urls)
		}
		uris := []string{}
		for _, u := range urls[start:end] {
			uris = append(uris, u.URI)
			if u.Origin != "" {
				uris = append(uris, u.Origin)
			}
		}
		if e := s.policy.GuardMany(uris, func() error {
			return s.batch(ctx, func(tx *sql.Tx) error {
				for _, u := range urls[start:end] {
					if _, _, e := Normalize(u.URI); e != nil {
						continue
					}
					target, e := recordURL(ctx, tx, u.URI, false)
					if e != nil {
						return e
					}
					if u.Origin == "" {
						continue
					}
					_, sourceName, e := Normalize(u.Origin)
					if e != nil {
						continue
					}
					source, e := hostID(ctx, tx, sourceName)
					if e != nil {
						return e
					}
					if source != target {
						if _, e = tx.ExecContext(ctx, "INSERT OR IGNORE INTO edges(source,target) VALUES(?,?)", source, target); e != nil {
							return e
						}
					}
				}
				return nil
			})
		}); e != nil {
			if errors.Is(e, protection.ErrBlocked) {
				return protection.ErrChanged
			}
			return e
		}
	}
	return nil
}
func (s *Store) batch(ctx context.Context, fn func(*sql.Tx) error) error {
	s.write.Lock()
	defer s.write.Unlock()
	return transaction(ctx, s.db, fn)
}
func (s *Store) Response(ctx context.Context, uri string, status int) error {
	if e := s.policy.Check(uri); e != nil {
		if errors.Is(e, protection.ErrBlocked) {
			return nil
		}
		return e
	}
	if status < 100 || status > 599 {
		return nil
	}
	return s.policy.Guard(uri, func() error {
		return s.coalesced(ctx, func(tx *sql.Tx) error { _, e := recordURL(ctx, tx, uri, true); return e })
	})
}
func (s *Store) Counts(ctx context.Context) (Counts, error) {
	var c Counts
	e := s.db.QueryRowContext(ctx, `SELECT known,visited,known-visited,host_count,edge_count,revision,partial FROM meta WHERE id=1`).Scan(&c.Known, &c.Visited, &c.NotVisited, &c.Hosts, &c.Edges, &c.Revision, &c.Partial)
	return c, e
}
func (s *Store) Search(ctx context.Context, q string, after string, limit int) ([]Host, error) {

	q = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(q)), ".")
	if len(q) > 1024 {
		return nil, fmt.Errorf("host query too long")
	}
	if strings.Contains(q, "://") {
		_, host, e := Normalize(q)
		if e != nil {
			return []Host{}, nil
		}
		q = host
	} else {
		if host, _, e := net.SplitHostPort(q); e == nil {
			q = host
		}
		q = strings.Trim(q, "[]")
		if ascii, e := idna.Lookup.ToASCII(q); e == nil {
			q = ascii
		}
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	query := `WITH matches AS MATERIALIZED (
 SELECT h.id,h.name,h.known,h.visited,` + hostConnections + ` AS connections
 FROM hosts h WHERE h.name>=? AND h.name<?
 ) SELECT id,name,known,visited,connections FROM matches`
	args := []interface{}{q, q + "\uffff"}
	if after != "" {
		count, name, e := s.searchCursor(ctx, after)
		if e != nil {
			return nil, e
		}
		query += " WHERE connections<? OR (connections=? AND name>?)"
		args = append(args, count, count, name)
	}
	query += " ORDER BY connections DESC,name ASC LIMIT ?"
	args = append(args, limit)
	rows, e := s.db.QueryContext(ctx, query, args...)

	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Host{}
	for rows.Next() {
		var h Host
		if e = rows.Scan(&h.ID, &h.Name, &h.Known, &h.Visited, &h.Connections); e != nil {
			return nil, e
		}
		h.Cursor = hostCursor(h)
		out = append(out, h)
	}
	return out, rows.Err()
}
func (s *Store) Backfilled(ctx context.Context) (bool, error) {
	var b bool
	e := s.db.QueryRowContext(ctx, "SELECT backfilled FROM meta WHERE id=1").Scan(&b)
	return b, e
}
func (s *Store) FinishBackfill(ctx context.Context) error {
	s.write.Lock()
	defer s.write.Unlock()
	_, e := s.db.ExecContext(ctx, "UPDATE meta SET backfilled=1 WHERE id=1")
	return e
}

type Event struct {
	URL
	Status int
}

// Import coalesces historical receipt/discovery updates in bounded durable
// transactions. An interrupted batch can be replayed without double counting.
func (s *Store) Import(ctx context.Context, events []Event) error {
	for start := 0; start < len(events); start += 2048 {
		end := start + 2048
		if end > len(events) {
			end = len(events)
		}
		if e := s.batch(ctx, func(tx *sql.Tx) error {
			for _, v := range events[start:end] {
				if _, _, e := Normalize(v.URI); e != nil {
					continue
				}
				target, e := recordURL(ctx, tx, v.URI, v.Status >= 100 && v.Status <= 599)
				if e != nil {
					return e
				}
				if v.Origin != "" {
					_, name, e := Normalize(v.Origin)
					if e == nil {
						source, e := hostID(ctx, tx, name)
						if e != nil {
							return e
						}
						if source != target {
							if _, e = tx.ExecContext(ctx, "INSERT OR IGNORE INTO edges(source,target) VALUES(?,?)", source, target); e != nil {
								return e
							}
						}
					}
				}
			}
			return nil
		}); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) Progress(ctx context.Context, key string) (string, error) {
	var value string
	e := s.db.QueryRowContext(ctx, "SELECT value FROM progress WHERE key=?", key).Scan(&value)
	if e == sql.ErrNoRows {
		return "", nil
	}
	return value, e
}
func (s *Store) Checkpoint(ctx context.Context, key, value string) error {
	s.write.Lock()
	defer s.write.Unlock()
	_, e := s.db.ExecContext(ctx, "INSERT INTO progress(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value)
	return e
}
