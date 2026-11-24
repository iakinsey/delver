package frontier

import (
	"context"
	"encoding/json"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/repetition"
	"github.com/iakinsey/delver/site"
	"strings"
)

// Classification is disposable derived metadata. If protection removes any
// supporting site, invalidate that family instead of retaining partial evidence.
func (s *Store) purgeRepeated(ctx context.Context, match func(string) bool) error {
	if !s.hasRepeated.Load() {
		return nil
	}
	for _, prefix := range []string{"repeated/family/", "repeated/page/", "repeated/source/", "repeated/site/", "repeated/member/", "repeated/projection/"} {
		cursor := ""
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			n := 0
			s.mu.Lock()
			err := s.update(func(tx *badger.Txn) error {
				it := tx.NewIterator(badger.DefaultIteratorOptions)
				defer it.Close()
				start := prefix
				if cursor != "" {
					start = cursor + "\x00"
				}
				for it.Seek([]byte(start)); it.ValidForPrefix([]byte(prefix)) && n < 128; it.Next() {
					key := string(it.Item().Key())
					cursor = key
					n++
					var raw []byte
					if err := it.Item().Value(func(b []byte) error { raw = append([]byte{}, b...); return nil }); err != nil {
						return err
					}
					changed := false
					for _, part := range strings.Split(key, "/") {
						if strings.Contains(part, ".") && match(site.HTTPURL(part)) {
							changed = true
						}
					}
					if json.Valid(raw) {
						_, diff, err := protection.SanitizeJSON(raw, match)
						if err != nil {
							return err
						}
						changed = changed || diff
					}
					if prefix == "repeated/projection/" {
						var u repetition.Update
						if err := json.Unmarshal(raw, &u); err != nil {
							return err
						}
						for _, f := range u.Families {
							if !f.Confirmed && f.Sites == 0 && f.Representative == "" {
								continue
							}
							if _, err := tx.Get([]byte("repeated/family/" + f.ID)); err == badger.ErrKeyNotFound {
								changed = true
							} else if err != nil {
								return err
							}
						}
					}
					if !changed {
						continue
					}
					if err := tx.Delete([]byte(key)); err != nil {
						return err
					}
					if prefix == "repeated/family/" {
						var f repeatedFamily
						if err := json.Unmarshal(raw, &f); err != nil {
							return err
						}
						if err := s.enqueueRepeatedProjection(tx, repetition.Update{Families: []repetition.Family{{ID: f.ID, Epoch: s.repeatedEpoch}}}); err != nil {
							return err
						}
					}
				}
				return nil
			})
			s.mu.Unlock()
			if err != nil {
				return err
			}
			if n < 128 {
				break
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.update(s.markRepeatedRouting)
}
