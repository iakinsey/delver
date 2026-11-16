package frontier

import (
	"database/sql"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	_ "github.com/mattn/go-sqlite3"
	"path/filepath"
	"testing"
)

// Compare the same atomic record/index transitions with durable writes enabled.
func BenchmarkStorageTransitions(b *testing.B) {
	for _, engine := range []string{"badger", "sqlite"} {
		b.Run(engine, func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "db")
			var apply func(string) error
			if engine == "badger" {
				db, err := badger.Open(badger.DefaultOptions(path).WithLogger(nil).WithSyncWrites(true))
				if err != nil {
					b.Fatal(err)
				}
				defer db.Close()
				apply = func(key string) error {
					return db.Update(func(tx *badger.Txn) error {
						if err := tx.Set([]byte("u/"+key), []byte("completed")); err != nil {
							return err
						}
						if err := tx.Delete([]byte("p/" + key)); err != nil {
							return err
						}
						return tx.Set([]byte("e/"+key), []byte(key))
					})
				}
			} else {
				db, err := sql.Open("sqlite3", path)
				if err != nil {
					b.Fatal(err)
				}
				defer db.Close()
				if _, err = db.Exec("PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; CREATE TABLE kv(k TEXT PRIMARY KEY,v BLOB) WITHOUT ROWID"); err != nil {
					b.Fatal(err)
				}
				apply = func(key string) error {
					tx, err := db.Begin()
					if err != nil {
						return err
					}
					defer tx.Rollback()
					for _, pair := range [][2]string{{"u/" + key, "completed"}, {"e/" + key, key}} {
						if _, err = tx.Exec("INSERT OR REPLACE INTO kv VALUES(?,?)", pair[0], pair[1]); err != nil {
							return err
						}
					}
					if _, err = tx.Exec("DELETE FROM kv WHERE k=?", "p/"+key); err != nil {
						return err
					}
					return tx.Commit()
				}
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := apply(fmt.Sprintf("%020d", i)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
