package frontier

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/types/message"
	"strings"
	"time"
)

type revisitEntry struct {
	message.FetcherRequest
	Origins      []int64 `json:"origins,omitempty"`
	NormalPinned bool    `json:"normal_pinned,omitempty"`
}

// DrainRecrawls makes completed work eligible independently of dedup retention.
// The due entry owns future work until its replacement is durably admitted.
func (s *Store) DrainRecrawls(ctx context.Context, limit int) error {
	if s.params.RevisitAfter == 0 {
		return nil
	}
	type due struct {
		key     string
		request revisitEntry
	}
	entries := []due{}
	err := s.view(func(tx *badger.Txn) error {
		var pending int
		if err := read(tx, "meta/pending", &pending); err != nil && err != badger.ErrKeyNotFound {
			return err
		}
		// Revisit ownership stays durable while capacity is full. Reading the
		// entire due window here would delay dispatch of already pending work.
		available := s.params.MaxPending - pending
		if available <= 0 {
			return nil
		}
		if limit > available {
			limit = available
		}
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := tx.NewIterator(opts)
		defer it.Close()
		for it.Seek([]byte("revisit/")); it.ValidForPrefix([]byte("revisit/")) && len(entries) < limit; it.Next() {
			key := string(it.Item().Key())
			var deadline int64
			if _, err := fmt.Sscanf(key, "revisit/%d/", &deadline); err != nil {
				return err
			}
			if deadline > time.Now().UnixNano() {
				break
			}
			var r revisitEntry
			if err := it.Item().Value(func(b []byte) error { return json.Unmarshal(b, &r) }); err != nil {
				return err
			}
			entries = append(entries, due{key, r})
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		active := false
		err := func() error {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.update(func(tx *badger.Txn) error {
				if s.trapBlocked(tx, e.request.URI) {
					active = true
					return nil
				}
				var c Candidate
				if err := read(tx, "u/"+e.request.URI, &c); err == nil {
					if c.Terminal == 0 {
						if err := s.mergeRevisitProvenance(tx, &c, e.request); err != nil {
							return err
						}
					}
					if c.Terminal == 0 || c.Request.RequestID != e.request.RequestID {
						active = true
						return nil
					}
					if err := tx.Delete([]byte("u/" + c.Request.URI)); err != nil {
						return err
					}
					for _, pair := range []struct {
						life Lifetime
						kind string
					}{{s.params.Retention.CompletedFor, "detail"}, {s.params.Retention.FailedFor, "detail"}, {s.params.Retention.DedupFor, "dedup"}} {
						if pair.life != 0 {
							if err := tx.Delete([]byte(expiry(retentionDeadline(c.Terminal, pair.life), pair.kind, c.Request.URI))); err != nil {
								return err
							}
						}
					}
				} else if err != badger.ErrKeyNotFound {
					return err
				}
				return nil
			})
		}()
		if err != nil {
			return err
		}
		if !active {
			r := e.request.FetcherRequest
			r.Attempt = 0
			r.RequestID = ""
			r.FrontierGeneration = ""
			if err := s.Admit(ctx, r); err == ErrCapacity {
				return nil
			} else if err != nil && err != ErrRejected {
				return err
			}
			s.mu.Lock()
			err := s.update(func(tx *badger.Txn) error {
				var c Candidate
				if err := read(tx, "u/"+r.URI, &c); err == badger.ErrKeyNotFound {
					return nil
				} else if err != nil {
					return err
				}
				return s.mergeRevisitProvenance(tx, &c, e.request)
			})
			s.mu.Unlock()
			if err != nil {
				return err
			}
		}
		if err := s.update(func(tx *badger.Txn) error { return tx.Delete([]byte(e.key)) }); err != nil {
			return err
		}
	}
	return nil
}

func revisitKey(timestamp int64, uri string) string {
	return fmt.Sprintf("revisit/%020d/%s", timestamp, strings.TrimSpace(uri))
}

func (s *Store) mergeRevisitProvenance(tx *badger.Txn, c *Candidate, entry revisitEntry) error {
	if !s.hasRepeated.Load() {
		return nil
	}
	old := *c
	c.NormalPinned = c.NormalPinned || entry.NormalPinned
	for _, id := range entry.Origins {
		found := false
		for _, existing := range c.Origins {
			if existing == id {
				found = true
				break
			}
		}
		if !found {
			if len(c.Origins) >= 128 {
				c.NormalPinned = true
				break
			}
			c.Origins = append(c.Origins, id)
		}
	}
	repeated, err := s.candidateRepeated(tx, *c)
	if err != nil {
		return err
	}
	c.Repeated = repeated
	if c.Repeated != old.Repeated && (c.State == "pending" || c.State == "reserved") {
		if err := schedule(tx, old, true); err != nil {
			return err
		}
		if err := schedule(tx, *c, false); err != nil {
			return err
		}
	}
	return write(tx, "u/"+c.Request.URI, *c)
}
