package frontier

import (
	"encoding/json"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/util"
	"strings"
)

// The frontier is not exposed until migration completes. Every 128-key chunk
// commits its continuation with its changes, so interrupted startup is replay safe.
func (s *Store) ensureSiteIndexes() error {
	var complete bool
	if e := s.db.View(func(tx *badger.Txn) error {
		_, e := tx.Get([]byte("meta/site_index_v1"))
		complete = e == nil
		if e == badger.ErrKeyNotFound {
			return nil
		}
		return e
	}); e != nil || complete {
		return e
	}
	for _, prefix := range []string{"origin/", "budget/origin/", "p/"} {
		for {
			done := false
			e := s.db.Update(func(tx *badger.Txn) error {
				cursor := ""
				if e := read(tx, "migration/site/"+prefix, &cursor); e != nil && e != badger.ErrKeyNotFound {
					return e
				}
				opts := badger.DefaultIteratorOptions
				opts.PrefetchValues = false
				it := tx.NewIterator(opts)
				defer it.Close()
				start := prefix
				if cursor != "" {
					start = cursor + "\x00"
				}
				n := 0
				last := cursor
				for it.Seek([]byte(start)); it.ValidForPrefix([]byte(prefix)) && n < 128; it.Next() {
					key := string(it.Item().Key())
					last = key
					n++
					if prefix == "p/" {
						var uri string
						if e := read(tx, key, &uri); e != nil {
							return e
						}
						var c Candidate
						if e := read(tx, "u/"+uri, &c); e != nil {
							return e
						}
						keys := schedulingKeys(c)
						for _, k := range keys {
							if strings.HasPrefix(k, "hs/") || strings.HasPrefix(k, "fhs/") {
								if e := write(tx, k, uri); e != nil {
									return e
								}
							}
						}
					} else {
						var count int
						if e := read(tx, key, &count); e != nil {
							return e
						}
						target := "site_pending/"
						if prefix == "budget/origin/" {
							target = "budget/site/"
						}
						if e := increment(tx, target+util.SiteKey(strings.TrimPrefix(key, prefix)), int64(count)); e != nil {
							return e
						}
					}
				}
				done = !it.ValidForPrefix([]byte(prefix))
				return write(tx, "migration/site/"+prefix, last)
			})
			if e != nil {
				return fmt.Errorf("site index migration: %w", e)
			}
			if done {
				break
			}
		}
	}
	// Legacy scheduling and counters no longer have readers; remove them in bounded chunks.
	for _, prefix := range []string{"h/", "fh/", "origin/", "budget/origin/", "migration/site/"} {
		for {
			count := 0
			e := s.db.Update(func(tx *badger.Txn) error {
				opts := badger.DefaultIteratorOptions
				opts.PrefetchValues = false
				it := tx.NewIterator(opts)
				defer it.Close()
				for it.Seek([]byte(prefix)); it.ValidForPrefix([]byte(prefix)) && count < 128; it.Next() {
					if e := tx.Delete(it.Item().KeyCopy(nil)); e != nil {
						return e
					}
					count++
				}
				return nil
			})
			if e != nil {
				return e
			}
			if count < 128 {
				break
			}
		}
	}
	raw, _ := json.Marshal(true)
	return s.db.Update(func(tx *badger.Txn) error { return tx.Set([]byte("meta/site_index_v1"), raw) })
}

// Import the strongest existing EMA once, without multiplying a site's credit
// by its number of hostnames. Subsequent rewards update one canonical EMA.
func (s *Store) ensureSiteRewards() error {
	var done bool
	if e := s.db.View(func(tx *badger.Txn) error {
		_, e := tx.Get([]byte("meta/site_rewards_v1"))
		done = e == nil
		if e == badger.ErrKeyNotFound {
			return nil
		}
		return e
	}); e != nil || done {
		return e
	}
	cursor := ""
	for {
		count := 0
		last := cursor
		e := s.db.Update(func(tx *badger.Txn) error {
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			it := tx.NewIterator(opts)
			defer it.Close()
			start := "reward/"
			if cursor != "" {
				start = cursor + "\x00"
			}
			for it.Seek([]byte(start)); it.ValidForPrefix([]byte("reward/")) && count < 128; it.Next() {
				key := string(it.Item().Key())
				count++
				last = key
				var old, current rewardStats
				if e := read(tx, key, &old); e != nil {
					return e
				}
				target := "site_reward/" + util.SiteKey(strings.TrimPrefix(key, "reward/"))
				if e := read(tx, target, &current); e != nil && e != badger.ErrKeyNotFound {
					return e
				}
				if old.Value > current.Value || current.LastHash == "" {
					if e := write(tx, target, old); e != nil {
						return e
					}
				}
			}
			return nil
		})
		if e != nil {
			return e
		}
		cursor = last
		if count < 128 {
			break
		}
	}
	return s.db.Update(func(tx *badger.Txn) error { return write(tx, "meta/site_rewards_v1", true) })
}
