package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/types"
	"io"
	"os"
	"path/filepath"
)

func payloadMatches(m types.Message, match func(string) bool) bool {
	for _, u := range protection.PayloadURLs(m.Message) {
		if match(u) {
			return true
		}
	}
	return false
}
func (s *fileQueue) PurgeMatching(ctx context.Context, match func(string) bool) error {
	s.putLock.Lock()
	defer s.putLock.Unlock()
	defer s.invalidateCandidatesLocked()
	for _, root := range []string{s.path, s.dlqPath} {
		if root == "" {
			continue
		}
		dir, e := os.Open(root)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		for {
			entries, e := dir.ReadDir(128)
			for _, entry := range entries {
				if x := ctx.Err(); x != nil {
					dir.Close()
					return x
				}
				if entry.IsDir() || entry.Name() == ".lock" {
					continue
				}
				if entry.Type()&os.ModeType != 0 {
					dir.Close()
					return fmt.Errorf("unsupported queue file prevents cleanup")
				}
				path := filepath.Join(root, entry.Name())
				raw, x := os.ReadFile(path)
				if os.IsNotExist(x) {
					continue
				}
				if x != nil {
					dir.Close()
					return x
				}
				if !json.Valid(raw) {
					dir.Close()
					return fmt.Errorf("invalid queue record prevents purge verification")
				}
				var m types.Message
				own := json.Unmarshal(raw, &m) == nil && payloadMatches(m, match)
				if x = protection.RewriteJSONFile(ctx, path, match); x != nil && !os.IsNotExist(x) {
					dir.Close()
					return x
				}
				if own {
					if e := os.Remove(path + ".outputs"); e != nil && !os.IsNotExist(e) {
						dir.Close()
						return e
					}
				}
			}
			if e != nil {
				if e != io.EOF {
					dir.Close()
					return e
				}
				break
			}
		}
		if e := dir.Sync(); e != nil {
			dir.Close()
			return e
		}
		dir.Close()
	}
	return nil
}
func (s *crawlQueue) PurgeMatching(ctx context.Context, match func(string) bool) error {
	cursor := []byte(nil)
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		count := 0
		last := cursor
		e := s.update(ctx, func(tx *badger.Txn) error {
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			it := tx.NewIterator(opts)
			defer it.Close()
			start := cursor
			if len(start) > 0 {
				start = append(append([]byte{}, cursor...), 0)
			}
			for it.Seek(start); it.Valid() && count < 128; it.Next() {
				key := it.Item().KeyCopy(nil)
				last = key
				count++
				var record crawlRecord
				var value []byte
				if e := it.Item().Value(func(b []byte) error { value = append([]byte{}, b...); return nil }); e != nil {
					return e
				}
				if !json.Valid(value) {
					continue
				}
				own := json.Unmarshal(value, &record) == nil && record.Message.ID != "" && payloadMatches(record.Message, match)
				if !own {
					clean, changed, e := protection.SanitizeJSON(value, match)
					if e != nil {
						return e
					}
					if changed {
						if clean == nil {
							if e = tx.Delete(key); e != nil {
								return e
							}
						} else if e = tx.Set(key, clean); e != nil {
							return e
						}
					}
					continue
				}
				if _, e := tx.Get([]byte("active_ready\x00" + s.params.CrawlID)); e == nil {
					if e := s.adjustCount(tx, record.State, -1); e != nil {
						return e
					}
				} else if e != badger.ErrKeyNotFound {
					return e
				}
				for _, k := range [][]byte{key, s.pendingKey(key), append([]byte("active\x00"), key...), append([]byte("outputs\x00"), key...)} {
					if e := tx.Delete(k); e != nil {
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
			return nil
		}
	}
}
