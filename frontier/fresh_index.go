package frontier

import "github.com/dgraph-io/badger/v3"

// Upgrade only the active request index, never completed URL history. A missing
// completion marker safely rebuilds the idempotent index after interrupted startup.
func (s *Store) ensureFreshIndexes() error {
	complete := false
	err := s.db.View(func(tx *badger.Txn) error {
		_, err := tx.Get([]byte("meta/fresh_index_v1"))
		if err == nil {
			complete = true
			return nil
		}
		if err == badger.ErrKeyNotFound {
			return nil
		}
		return err
	})
	if err != nil || complete {
		return err
	}
	// Use bounded transactions rather than a WriteBatch. Badger v3's empty
	// WriteBatch.Flush replaces its transaction without discarding the old one,
	// pinning the read watermark (and all subsequent versions) on an empty DB.
	// Each chunk commits its cursor with its index entries before startup proceeds.
	for {
		done := false
		err = s.db.Update(func(tx *badger.Txn) error {
			cursor := ""
			if err := read(tx, "migration/fresh_index_cursor_v1", &cursor); err != nil && err != badger.ErrKeyNotFound {
				return err
			}
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			it := tx.NewIterator(opts)
			defer it.Close()
			start := "p/"
			if cursor != "" {
				start = cursor + "\x00"
			}
			count := 0
			for it.Seek([]byte(start)); it.ValidForPrefix([]byte("p/")) && count < 128; it.Next() {
				cursor = string(it.Item().Key())
				count++
				var uri string
				if err := read(tx, cursor, &uri); err != nil {
					return err
				}
				var c Candidate
				if err := read(tx, "u/"+uri, &c); err != nil {
					return err
				}
				if c.State != "pending" || c.Request.Attempt != 0 {
					continue
				}
				for _, key := range schedulingKeys(c) {
					if key[0] == 'f' {
						if err := write(tx, key, uri); err != nil {
							return err
						}
					}
				}
			}
			done = !it.ValidForPrefix([]byte("p/"))
			if !done {
				return write(tx, "migration/fresh_index_cursor_v1", cursor)
			}
			if err := tx.Delete([]byte("migration/fresh_index_cursor_v1")); err != nil {
				return err
			}
			return tx.Set([]byte("meta/fresh_index_v1"), []byte{1})
		})
		if err != nil || done {
			return err
		}
	}
}
