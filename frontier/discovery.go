package frontier

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/types/message"
	"strconv"
	"strings"
	"time"
)

// StageDiscovery durably transfers discovery ownership from an analysis inbox.
// Chunk markers make replay safe without a transaction proportional to link count.
func (s *Store) StageDiscovery(ctx context.Context, id string, requests []message.FetcherRequest) error {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(id)))
	for start := 0; start < len(requests); start += 128 {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + 128
		if end > len(requests) {
			end = len(requests)
		}
		err := func() error {
			s.mu.Lock()
			defer s.mu.Unlock()
			if err := s.checkDisk(); err != nil {
				return err
			}
			return s.db.Update(func(tx *badger.Txn) error {
				marker := fmt.Sprintf("discovery/%s/%08d", digest, start)
				if _, err := tx.Get([]byte(marker)); err == nil {
					return nil
				} else if err != badger.ErrKeyNotFound {
					return err
				}
				for i := start; i < end; i++ {
					key := fmt.Sprintf("staged/%s/%08d", digest, i)
					if err := write(tx, key, requests[i]); err != nil {
						return err
					}
				}
				if err := increment(tx, "meta/staged", int64(end-start)); err != nil {
					return err
				}
				return write(tx, marker, end-start)
			})
		}()
		if err != nil {
			return s.storageError(err)
		}
	}
	return nil
}

// DrainDiscovery retries soft admission pressure without consuming input retries.
// A persisted key cursor rotates past full origins, including after key deletion.
func (s *Store) DrainDiscovery(ctx context.Context, limit int) (int, error) {
	type entry struct {
		key     string
		request message.FetcherRequest
	}
	entries := []entry{}
	err := s.db.View(func(tx *badger.Txn) error {
		var cursor string
		if err := read(tx, "meta/staged_cursor", &cursor); err != nil && err != badger.ErrKeyNotFound {
			return err
		}
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := tx.NewIterator(opts)
		defer it.Close()
		scan := func(start, stop string) error {
			for it.Seek([]byte(start)); it.ValidForPrefix([]byte("staged/")) && len(entries) < limit; it.Next() {
				key := string(it.Item().Key())

				if stop != "" && key >= stop {
					break
				}
				var r message.FetcherRequest
				if err := it.Item().Value(func(b []byte) error { return json.Unmarshal(b, &r) }); err != nil {
					return err
				}
				entries = append(entries, entry{key, r})
			}
			return nil
		}
		if cursor != "" {
			if err := scan(cursor+"\x00", ""); err != nil {
				return err
			}
		}
		if len(entries) < limit {
			stop := ""
			if cursor != "" {
				stop = cursor + "\x00"
			}
			return scan("staged/", stop)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	admitted := 0
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return admitted, err
		}
		result := s.Admit(ctx, e.request)
		if result != nil && !errors.Is(result, ErrCapacity) && !errors.Is(result, ErrRejected) {
			return admitted, result
		}
		err := func() error {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.db.Update(func(tx *badger.Txn) error {
				if err := write(tx, "meta/staged_cursor", e.key); err != nil {
					return err
				}
				if errors.Is(result, ErrCapacity) {
					return increment(tx, "meta/capacity_deferrals", 1)
				}
				if _, err := tx.Get([]byte(e.key)); err == badger.ErrKeyNotFound {
					return nil
				} else if err != nil {
					return err
				}
				if errors.Is(result, ErrRejected) {
					if err := increment(tx, "meta/budget_rejected", 1); err != nil {
						return err
					}
				}
				if err := tx.Delete([]byte(e.key)); err != nil {
					return err
				}
				parts := strings.Split(e.key, "/")
				index, err := strconv.Atoi(parts[2])
				if err != nil {
					return err
				}
				marker := fmt.Sprintf("discovery/%s/%08d", parts[1], index/128*128)
				var remaining int
				if err := read(tx, marker, &remaining); err != nil {
					return err
				}
				remaining--
				if remaining < 0 {
					return fmt.Errorf("invalid discovery chunk count")
				}
				if remaining == 0 {
					if err := writeTTL(tx, marker, 0, time.Duration(s.params.Retention.DedupFor)); err != nil {
						return err
					}
				} else if err := write(tx, marker, remaining); err != nil {
					return err
				}
				return increment(tx, "meta/staged", -1)
			})
		}()
		if err != nil {
			return admitted, err
		}
		if result == nil {
			admitted++
		}
	}
	return admitted, nil
}
