package frontier

import (
	"encoding/json"
	"github.com/dgraph-io/badger/v3"
)

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
	batch := s.db.NewWriteBatch()
	defer batch.Cancel()
	err = s.db.View(func(tx *badger.Txn) error {
		it := tx.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek([]byte("p/")); it.ValidForPrefix([]byte("p/")); it.Next() {
			var uri string
			if err := it.Item().Value(func(raw []byte) error { return json.Unmarshal(raw, &uri) }); err != nil {
				return err
			}
			var c Candidate
			if err := read(tx, "u/"+uri, &c); err != nil {
				return err
			}
			if c.State != "pending" || c.Request.Attempt != 0 {
				continue
			}
			raw, err := json.Marshal(uri)
			if err != nil {
				return err
			}
			for _, key := range schedulingKeys(c) {
				if key[0] == 'f' {
					if err := batch.Set([]byte(key), raw); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err = batch.Flush(); err != nil {
		return err
	}
	return s.db.Update(func(tx *badger.Txn) error { return tx.Set([]byte("meta/fresh_index_v1"), []byte{1}) })
}
