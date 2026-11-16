package frontier

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"
)

type workloadDB interface {
	put(map[string][]byte, []string) error
	get(string) ([]byte, error)
	scan(string, int) ([]string, error)
	close() error
}
type badgerWorkload struct{ db *badger.DB }

func (d badgerWorkload) put(values map[string][]byte, deletes []string) error {
	return d.db.Update(func(tx *badger.Txn) error {
		for k, v := range values {
			if err := tx.Set([]byte(k), v); err != nil {
				return err
			}
		}
		for _, k := range deletes {
			if err := tx.Delete([]byte(k)); err != nil {
				return err
			}
		}
		return nil
	})
}
func (d badgerWorkload) get(k string) ([]byte, error) {
	var out []byte
	err := d.db.View(func(tx *badger.Txn) error {
		item, err := tx.Get([]byte(k))
		if err != nil {
			return err
		}
		out, err = item.ValueCopy(nil)
		return err
	})
	return out, err
}
func (d badgerWorkload) scan(prefix string, n int) ([]string, error) {
	out := []string{}
	err := d.db.View(func(tx *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := tx.NewIterator(opts)
		defer it.Close()
		for it.Seek([]byte(prefix)); it.ValidForPrefix([]byte(prefix)) && len(out) < n; it.Next() {
			out = append(out, string(it.Item().Key()))
		}
		return nil
	})
	return out, err
}
func (d badgerWorkload) close() error { return d.db.Close() }

type sqliteWorkload struct{ db *sql.DB }

func (d sqliteWorkload) put(values map[string][]byte, deletes []string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range values {
		if _, err := tx.Exec("INSERT OR REPLACE INTO kv VALUES(?,?)", k, v); err != nil {
			return err
		}
	}
	for _, k := range deletes {
		if _, err := tx.Exec("DELETE FROM kv WHERE k=?", k); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (d sqliteWorkload) get(k string) ([]byte, error) {
	var value []byte
	err := d.db.QueryRow("SELECT v FROM kv WHERE k=?", k).Scan(&value)
	return value, err
}
func (d sqliteWorkload) scan(prefix string, n int) ([]string, error) {
	rows, err := d.db.Query("SELECT k FROM kv WHERE k>=? AND k<? ORDER BY k LIMIT ?", prefix, prefix+"\xff", n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
func (d sqliteWorkload) close() error { return d.db.Close() }
func openWorkload(engine, path string) (workloadDB, error) {
	if engine == "badger" {
		db, err := badger.Open(frontierOptions(path, 64))
		if err != nil {
			return nil, err
		}
		return badgerWorkload{db}, nil
	}
	db, err := sql.Open("sqlite3", path+".sqlite")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA cache_size=-16384; CREATE TABLE IF NOT EXISTS kv(k TEXT PRIMARY KEY,v BLOB) WITHOUT ROWID"); err != nil {
		db.Close()
		return nil, err
	}
	return sqliteWorkload{db}, nil
}
func diskUsage(path string) int64 {
	var n int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n += info.Size()
		}
		return nil
	})
	return n
}

// BenchmarkStorageWorkload runs the same durable admission, ordered selection,
// reservation, publication, outcome, compaction, deletion, and reopen operations.
func BenchmarkStorageWorkload(b *testing.B) {
	for _, engine := range []string{"badger", "sqlite"} {
		for _, history := range []int{0, 10000} {
			b.Run(fmt.Sprintf("%s/history_%d", engine, history), func(b *testing.B) {
				root := b.TempDir()
				path := filepath.Join(root, "db")
				db, err := openWorkload(engine, path)
				if err != nil {
					b.Fatal(err)
				}
				defer func() {
					if db != nil {
						db.close()
					}
				}()
				payload := bytes.Repeat([]byte("x"), 512)
				for start := 0; start < history; start += 128 {
					values := map[string][]byte{}
					for i := start; i < start+128 && i < history; i++ {
						values[fmt.Sprintf("u/history/%012d", i)] = payload
					}
					if err := db.put(values, nil); err != nil {
						b.Fatal(err)
					}
				}
				b.ResetTimer()
				var reads []time.Duration
				for i := 0; i < b.N; i++ {
					key := fmt.Sprintf("%012d", i)
					record := []byte(`{"state":"pending","depth":2}`)
					if err := db.put(map[string][]byte{"u/" + key: record, "p/" + key: nil, "h/host/" + key: nil}, nil); err != nil {
						b.Fatal(err)
					}
					start := time.Now()
					keys, err := db.scan("p/", 32)
					reads = append(reads, time.Since(start))
					if err != nil || len(keys) != 1 {
						b.Fatal(keys, err)
					}
					if err := db.put(map[string][]byte{"u/" + key: []byte(`{"state":"reserved"}`)}, nil); err != nil {
						b.Fatal(err)
					}
					if err := db.put(map[string][]byte{"u/" + key: []byte(`{"state":"dispatched"}`)}, []string{"p/" + key, "h/host/" + key}); err != nil {
						b.Fatal(err)
					}
					if err := db.put(map[string][]byte{"u/" + key: []byte(`{"state":"completed","elapsed_ms":25}`), "e/" + key: nil, "reward/host": []byte("0.04")}, nil); err != nil {
						b.Fatal(err)
					}
					if _, err := db.get("u/" + key); err != nil {
						b.Fatal(err)
					}
					if err := db.put(map[string][]byte{"u/" + key: []byte(`{"state":"dedup"}`)}, []string{"e/" + key}); err != nil {
						b.Fatal(err)
					}
					if err := db.put(nil, []string{"u/" + key}); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				sort.Slice(reads, func(i, j int) bool { return reads[i] < reads[j] })
				if len(reads) > 0 {
					b.ReportMetric(float64(reads[len(reads)*95/100].Nanoseconds()), "seek_p95_ns")
				}
				if err := db.put(map[string][]byte{"reservation/restart": []byte("stable-id")}, nil); err != nil {
					b.Fatal(err)
				}
				if err := db.close(); err != nil {
					b.Fatal(err)
				}
				db = nil
				start := time.Now()
				db, err = openWorkload(engine, path)
				if err != nil {
					b.Fatal(err)
				}
				value, err := db.get("reservation/restart")
				if err != nil || string(value) != "stable-id" {
					b.Fatal(string(value), err)
				}
				b.ReportMetric(float64(time.Since(start).Nanoseconds()), "reopen_ns")
				b.ReportMetric(float64(diskUsage(root)), "disk_bytes")
				var mem runtime.MemStats
				runtime.ReadMemStats(&mem)
				b.ReportMetric(float64(mem.HeapAlloc), "heap_bytes")
				report, _ := json.Marshal(map[string]interface{}{"backend": engine, "history": history, "iterations": b.N, "disk_bytes": diskUsage(root)})
				b.Log(string(report))
			})
		}
	}
}
