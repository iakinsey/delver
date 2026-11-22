package frontier

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/site"
	"github.com/iakinsey/delver/types/message"
	"net"
	"strings"
)

// PurgeMatching uses seek continuations rather than retaining the frontier.
func (s *Store) PurgeMatching(ctx context.Context, match func(string) bool) error {
	for _, prefix := range []string{"u/", "staged/", "revisit/", "e/", "retry/", "trap/", "cool/", "failures/", "reward/", "site_reward/", "budget/site/", "site_pending/"} {
		cursor := ""
		for {
			if e := ctx.Err(); e != nil {
				return e
			}
			count := 0
			last := cursor
			s.mu.Lock()
			e := s.update(func(tx *badger.Txn) error {
				opts := badger.DefaultIteratorOptions
				opts.PrefetchValues = false
				it := tx.NewIterator(opts)
				defer it.Close()
				start := prefix
				if cursor != "" {
					start = cursor + "\x00"
				}
				for it.Seek([]byte(start)); it.ValidForPrefix([]byte(prefix)) && count < 128; it.Next() {
					key := string(it.Item().Key())
					last = key
					count++
					switch prefix {
					case "u/":
						var c Candidate
						if e := read(tx, key, &c); e != nil {
							return e
						}
						if !match(c.Request.URI) {
							if c.Request.Origin != "" && match(c.Request.Origin) {
								c.Request.Origin = ""
								if e := write(tx, key, c); e != nil {
									return e
								}
							}
							continue
						}
						if e := schedule(tx, c, true); e != nil {
							return e
						}
						if e := tx.Delete([]byte(index(c))); e != nil {
							return e
						}
						if c.State == "reserved" {
							if e := tx.Delete([]byte(fmt.Sprintf("reserved/%020d/%s", c.Sequence, c.Request.URI))); e != nil {
								return e
							}
							if e := increment(tx, "meta/reserved", -1); e != nil {
								return e
							}
						}
						if c.State == "trap_parked" {
							if e := increment(tx, "meta/trap_parked", -1); e != nil {
								return e
							}
						}
						if c.Terminal == 0 && c.State != "trap_parked" {
							if e := increment(tx, "meta/pending", -1); e != nil {
								return e
							}
							if e := increment(tx, "site_pending/"+site.Key(c.Request.Host), -1); e != nil {
								return e
							}
						}
						if c.State == "retry_wait" {
							if e := increment(tx, "meta/retry_pending", -1); e != nil {
								return e
							}
						}
						if e := tx.Delete([]byte(key)); e != nil {
							return e
						}
					case "staged/":
						var r message.FetcherRequest
						if e := read(tx, key, &r); e != nil {
							return e
						}
						if match(r.URI) {
							if _, e := s.ackDiscovery(tx, discoveryEntry{key: key, request: r, purgeReceipt: true}); e != nil {
								return e
							}
						} else if r.Origin != "" && match(r.Origin) {
							r.Origin = ""
							if e := write(tx, key, r); e != nil {
								return e
							}
						}
					case "revisit/", "e/", "retry/":
						at := strings.Index(key, "http")
						if at >= 0 && match(key[at:]) {
							if e := tx.Delete([]byte(key)); e != nil {
								return e
							}
						}
					case "trap/":
						if strings.HasPrefix(key, "trap/staged/") {
							var request message.FetcherRequest
							if e := read(tx, key, &request); e != nil {
								return e
							}
							if match(request.URI) {
								original, e := parkedDiscoveryKey(key)
								if e != nil {
									return e
								}
								if e = s.finishDiscoveryReceipt(tx, original, true); e != nil {
									return e
								}
								if e = tx.Delete([]byte(key)); e != nil {
									return e
								}
								if e = increment(tx, "meta/trap_parked", -1); e != nil {
									return e
								}
								var cursor string
								if e = read(tx, "meta/parked_staged_cursor", &cursor); e != nil && e != badger.ErrKeyNotFound {
									return e
								}
								if strings.HasPrefix(cursor, "trap/staged/"+site.Key(request.URI)+"/") {
									if e = tx.Delete([]byte("meta/parked_staged_cursor")); e != nil {
										return e
									}
								}
							} else if request.Origin != "" && match(request.Origin) {
								request.Origin = ""
								if e := write(tx, key, request); e != nil {
									return e
								}
							}
							continue
						}
						if at := strings.Index(key, "http"); at >= 0 && match(key[at:]) {
							if e := tx.Delete([]byte(key)); e != nil {
								return e
							}
							continue
						}
						var raw []byte
						if e := it.Item().Value(func(b []byte) error { raw = append([]byte{}, b...); return nil }); e != nil {
							return e
						}
						clean, changed, e := protection.SanitizeJSON(raw, match)
						if e != nil {
							return e
						}

						if changed && strings.HasPrefix(key, "trap/family/") {
							var old, remaining TrapFamily
							if e = json.Unmarshal(raw, &old); e != nil {
								return e
							}
							if clean != nil {
								if e = json.Unmarshal(clean, &remaining); e != nil {
									return e
								}
							}
							if len(remaining.Observations) == 0 {
								for band := 0; band < 16 && len(old.Signature) == 64; band++ {
									index := fmt.Sprintf("trap/band/%02d/%016x%016x%016x%016x/%s", band, old.Signature[band*4], old.Signature[band*4+1], old.Signature[band*4+2], old.Signature[band*4+3], old.ID)
									if e = tx.Delete([]byte(index)); e != nil {
										return e
									}
								}
								if e = tx.Delete([]byte(key)); e != nil {
									return e
								}
								continue
							}
							remaining.URLs = nil
							clean, e = json.Marshal(remaining)
							if e != nil {
								return e
							}
						}
						parts := strings.Split(key, "/")
						if len(parts) > 2 && match(site.HTTPURL(parts[2])) {
							if e := tx.Delete([]byte(key)); e != nil {
								return e
							}
						} else if changed {
							if clean == nil {
								if e := tx.Delete([]byte(key)); e != nil {
									return e
								}
							} else if e := tx.Set([]byte(key), clean); e != nil {
								return e
							}
						}
					default:
						if match(site.HTTPURL(strings.TrimPrefix(key, prefix))) {
							if e := tx.Delete([]byte(key)); e != nil {
								return e
							}
						}
					}
				}
				return nil
			})
			s.mu.Unlock()
			if e != nil {
				return e
			}
			cursor = last
			if count < 128 {
				break
			}
		}
	}

	// This seek cursor contains a site key. Rewinding it preserves restoration
	// semantics and prevents a deleted site surviving as cursor metadata.
	s.mu.Lock()
	e := s.update(func(tx *badger.Txn) error {
		var cursor string
		if e := read(tx, "meta/park_cursor", &cursor); e != nil && e != badger.ErrKeyNotFound {
			return e
		}
		for _, part := range strings.Split(cursor, "/") {
			if (strings.Contains(part, ".") || net.ParseIP(part) != nil) && match(site.HTTPURL(part)) {
				return tx.Delete([]byte("meta/park_cursor"))
			}
		}
		return nil
	})
	s.mu.Unlock()
	return e
}

func (s *Store) VerifyNoMatches(ctx context.Context, match func(string) bool) error {
	return s.view(func(tx *badger.Txn) error {
		it := tx.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			if e := ctx.Err(); e != nil {
				return e
			}
			key := string(it.Item().Key())
			if at := strings.Index(key, "http"); at >= 0 && match(key[at:]) {
				return fmt.Errorf("matching frontier key remains")
			}
			for _, part := range strings.Split(key, "/") {
				if (strings.Contains(part, ".") || net.ParseIP(part) != nil) && match(site.HTTPURL(part)) {
					return fmt.Errorf("matching frontier site key remains")
				}
			}
			if e := it.Item().Value(func(raw []byte) error {
				if !json.Valid(raw) {
					return nil
				}
				_, changed, e := protection.SanitizeJSON(raw, match)
				if e != nil {
					return e
				}
				if changed {
					return fmt.Errorf("matching frontier value remains")
				}
				return nil
			}); e != nil {
				return e
			}
		}
		return nil
	})
}
