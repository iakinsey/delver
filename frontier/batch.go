package frontier

import (
	"errors"
	"github.com/dgraph-io/badger/v3"
)

// updateBounded treats a configured row limit as a ceiling, not as a promise
// that all rows fit one Badger transaction. Each row remains atomic; oversized
// transactions roll back and retry smaller chunks without dropping work.
func (s *Store) updateBounded(limit int, fn func(*badger.Txn, int) (int, error)) (int, error) {
	total, batch := 0, limit
	for total < limit {
		target := batch
		if left := limit - total; target > left {
			target = left
		}
		count := 0
		err := s.update(func(tx *badger.Txn) error { var err error; count, err = fn(tx, target); return err })
		if errors.Is(err, badger.ErrTxnTooBig) && target > 1 {
			batch = target / 2
			continue
		}
		if err != nil {
			return total, err
		}
		total += count
		if count < target {
			break
		}
	}
	return total, nil
}
