package frontier

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
)

func TestFreshIndexEmptyStartupReleasesReadSnapshot(t *testing.T) {
	s, err := OpenStore(StoreParams{Path: filepath.Join(t.TempDir(), "db")})
	require.NoError(t, err)
	defer s.Close()
	// A leaked startup transaction pins old versions even after compaction.
	for i := 0; i < 10; i++ {
		require.NoError(t, s.update(func(tx *badger.Txn) error {
			return write(tx, "version-probe", i)
		}))
	}
	require.NoError(t, s.update(func(tx *badger.Txn) error {
		return write(tx, "flush-marker/1", true)
	}))
	// DropPrefix flushes and rewrites tables without closing the database, so
	// reopening cannot conceal an outstanding read snapshot.
	require.NoError(t, s.db.DropPrefix([]byte("flush-marker/")))
	require.NoError(t, s.db.Flatten(1))
	require.NoError(t, s.db.View(func(tx *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.AllVersions = true
		it := tx.NewIterator(opts)
		defer it.Close()
		versions := 0
		for it.Seek([]byte("version-probe")); it.ValidForPrefix([]byte("version-probe")); it.Next() {
			versions++
		}
		require.Equal(t, 1, versions, "startup must not pin obsolete versions")
		return nil
	}))
}

func TestFreshIndexResumesCommittedChunk(t *testing.T) {
	p := StoreParams{Path: filepath.Join(t.TempDir(), "db")}
	s, err := OpenStore(p)
	require.NoError(t, err)
	for i := 0; i < 385; i++ {
		require.NoError(t, s.Admit(context.Background(), message.FetcherRequest{URI: fmt.Sprintf("https://example.test/%04d", i)}))
	}
	var cursor string
	require.NoError(t, s.db.Update(func(tx *badger.Txn) error {
		it := tx.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		count := 0
		for it.Seek([]byte("p/")); it.ValidForPrefix([]byte("p/")); it.Next() {
			key := string(it.Item().Key())
			var uri string
			require.NoError(t, read(tx, key, &uri))
			var c Candidate
			require.NoError(t, read(tx, "u/"+uri, &c))
			count++
			if count <= 128 {
				cursor = key
				continue
			}
			for _, key := range schedulingKeys(c) {
				if key[0] == 'f' {
					require.NoError(t, tx.Delete([]byte(key)))
				}
			}
		}
		require.NoError(t, tx.Delete([]byte("meta/fresh_index_v1")))
		return write(tx, "migration/fresh_index_cursor_v1", cursor)
	}))
	require.NoError(t, s.Close())
	s, err = OpenStore(p)
	require.NoError(t, err)
	defer s.Close()
	for _, prefix := range []string{"fb/", "fd/", "fhs/"} {
		require.Equal(t, 385, millionKeys(t, s, prefix), prefix)
	}
	require.NoError(t, s.db.View(func(tx *badger.Txn) error {
		_, err := tx.Get([]byte("migration/fresh_index_cursor_v1"))
		require.ErrorIs(t, err, badger.ErrKeyNotFound)
		_, err = tx.Get([]byte("meta/fresh_index_v1"))
		return err
	}))
}
