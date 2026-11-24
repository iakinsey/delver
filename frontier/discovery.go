package frontier

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"net/url"
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
		if e := s.policy.HealthError(); e != nil {
			return e
		}
		blocked := map[int]bool{}
		uris := []string{}
		for i := start; i < end; i++ {
			if requests[i].Depth < 0 || requests[i].Hops < 0 {
				return fmt.Errorf("negative traversal depth")
			}
			if _, e := Normalize(requests[i].URI, ""); e != nil {
				return e
			}
			blocked[i] = errors.Is(s.policy.Check(requests[i].URI), protection.ErrBlocked)
			if !blocked[i] {
				uris = append(uris, requests[i].URI)
				if requests[i].Origin != "" {
					uris = append(uris, requests[i].Origin)
				}
			}
		}
		err := s.policy.GuardMany(uris, func() error {
			s.mu.Lock()
			defer s.mu.Unlock()
			if err := s.checkDisk(); err != nil {
				return err
			}
			return s.update(func(tx *badger.Txn) error {
				marker := fmt.Sprintf("discovery/%s/%08d", digest, start)
				if _, err := tx.Get([]byte(marker)); err == nil {
					return nil
				} else if err != badger.ErrKeyNotFound {
					return err
				}
				remaining, skipped := 0, 0
				for i := start; i < end; i++ {
					if s.trapBlocked(tx, requests[i].URI) || blocked[i] || len(s.params.ExcludedExtensions) > 0 && util.URLFileExcluded(requests[i].URI, s.params.ExcludedExtensions) {
						// Preserve admission validation before accepting an excluded input.
						if requests[i].Depth < 0 || requests[i].Hops < 0 {
							return fmt.Errorf("negative traversal depth")
						}
						if _, err := Normalize(requests[i].URI, ""); err != nil {
							return err
						}
						skipped++
						continue
					}
					key := fmt.Sprintf("staged/%s/%08d", digest, i)
					if err := write(tx, key, requests[i]); err != nil {
						return err
					}
					remaining++
				}
				if skipped > 0 {
					if err := increment(tx, "meta/policy_skips", int64(skipped)); err != nil {
						return err
					}
				}
				if remaining == 0 {
					return writeTTL(tx, marker, 0, time.Duration(s.params.Retention.DedupFor))
				}
				if err := increment(tx, "meta/staged", int64(remaining)); err != nil {
					return err
				}
				if err := write(tx, marker, remaining); err != nil {
					return err
				}
				if s.hasRepeated.Load() {
					return s.routeRepeatedChunk(tx, marker)
				}
				return nil
			})
		})
		if errors.Is(err, protection.ErrBlocked) {
			return protection.ErrChanged
		}
		if err != nil {
			return s.storageError(err)
		}
	}
	return nil
}

// DrainDiscovery uses a bounded rotating window. Capacity hints are local to
// this pass; admit still enforces both limits in its own atomic transaction.
func (s *Store) DrainDiscovery(ctx context.Context, limit int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if limit < 1 {
		return 0, fmt.Errorf("discovery limit must be positive")
	}
	entries := []discoveryEntry{}
	hosts := map[string]int{}
	known := map[string]bool{}
	available := 0
	cursor := ""
	err := s.view(func(tx *badger.Txn) error {
		var staged int64
		if err := repeatedRead(tx, "meta/staged", &staged); err != nil {
			return err
		}
		if staged == 0 {
			return nil
		}
		var pending int
		if err := read(tx, "meta/pending", &pending); err != nil && err != badger.ErrKeyNotFound {
			return err
		}
		available = s.params.MaxPending - pending
		if available <= 0 {
			return nil
		}
		if s.hasRepeated.Load() {
			routed, ready, err := s.repeatedDiscoveryWindow(tx, limit)
			if err != nil {
				return err
			}
			if ready {
				for _, e := range routed {
					if e.stale {
						entries = append(entries, e)
						continue
					}
					uri, err := Normalize(e.request.URI, "")
					if err != nil {
						return err
					}
					e.request.URI = uri
					u, _ := url.Parse(uri)
					e.host = util.SiteKey(u.Host)
					if _, ok := hosts[e.host]; !ok {
						var n int
						if err := repeatedRead(tx, "site_pending/"+e.host, &n); err != nil {
							return err
						}
						hosts[e.host] = s.params.MaxPendingPerOrigin - n
					}
					if _, err := tx.Get([]byte("u/" + uri)); err == nil {
						known[uri] = true
					} else if err != badger.ErrKeyNotFound {
						return err
					}
					entries = append(entries, e)
				}
				return nil
			}
		}
		if err := read(tx, "meta/staged_cursor", &cursor); err != nil && err != badger.ErrKeyNotFound {
			return err
		}
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := tx.NewIterator(opts)
		defer it.Close()
		scan := func(start, stop string) error {
			for it.Seek([]byte(start)); it.ValidForPrefix([]byte("staged/")) && len(entries) < limit; it.Next() {
				if err := ctx.Err(); err != nil {
					return err
				}
				key := string(it.Item().Key())
				if stop != "" && key >= stop {
					break
				}
				var request message.FetcherRequest
				if err := it.Item().Value(func(b []byte) error { return json.Unmarshal(b, &request) }); err != nil {
					return err
				}
				uri, err := Normalize(request.URI, "")
				if err != nil {
					return err
				}
				request.URI = uri
				u, _ := url.Parse(uri)
				host := util.SiteKey(u.Host)
				if _, ok := hosts[host]; !ok {
					var n int
					if err := read(tx, "site_pending/"+host, &n); err != nil && err != badger.ErrKeyNotFound {
						return err
					}
					hosts[host] = s.params.MaxPendingPerOrigin - n
				}
				if _, err := tx.Get([]byte("u/" + uri)); err == nil {
					known[uri] = true
				} else if err != badger.ErrKeyNotFound {
					return err
				}
				entries = append(entries, discoveryEntry{key: key, request: request, host: host})
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
	if available <= 0 || len(entries) == 0 {
		return 0, nil
	}
	completed := []discoveryEntry{}
	consumed, deferrals := 0, 0
	last := ""
	attempted := [2]discoveryEntry{}
	var batched map[string]bool
	for at, e := range entries {
		if at%128 == 0 {
			end := min(len(entries), at+128)
			batched, err = s.batchAdmissions(ctx, entries[at:end], hosts, available, known)
			if err != nil {
				return consumed, err
			}
		}
		if err := ctx.Err(); err != nil {
			return consumed, err
		}
		if available <= 0 {
			break
		}
		last = e.key
		if e.route != "" {
			attempted[e.routeClass] = e
		}
		if e.stale {
			completed = append(completed, e)
			continue
		}
		// Old staged records may predate this policy. Count and remove them in
		// the same acknowledgment transaction, never through per-record admit.
		policyError := s.policy.Check(e.request.URI)
		if policyError != nil && !errors.Is(policyError, protection.ErrBlocked) {
			return consumed, policyError
		}
		if errors.Is(policyError, protection.ErrBlocked) || len(s.params.ExcludedExtensions) > 0 && util.URLFileExcluded(e.request.URI, s.params.ExcludedExtensions) {
			if e.request.Depth < 0 || e.request.Hops < 0 {
				return consumed, fmt.Errorf("negative traversal depth")
			}
			e.excluded = true
			consumed++
			completed = append(completed, e)
			continue
		}
		// Existing records may be duplicates or better paths needing no new slot.
		if hosts[e.host] <= 0 && !known[e.request.URI] {
			deferrals++
			continue
		}
		used := batched[e.key]
		var result error
		if batched == nil {
			used, result = s.admit(ctx, e.request)
		}
		if result == nil {
			known[e.request.URI] = true
			if used {
				available--
				hosts[e.host]--
			}
			e.noSlot = !used
			consumed++
			completed = append(completed, e)
		} else if errors.Is(result, ErrTrapExcluded) {
			e.parked = true
			completed = append(completed, e)
			consumed++
		} else if errors.Is(result, ErrRejected) {
			e.rejected = true
			completed = append(completed, e)
		} else if errors.Is(result, ErrCapacity) {
			deferrals++
			// Another caller may have filled either limit since our snapshot.
			if err := s.view(func(tx *badger.Txn) error {
				var pending, host int
				if err := read(tx, "meta/pending", &pending); err != nil && err != badger.ErrKeyNotFound {
					return err
				}
				if err := read(tx, "site_pending/"+e.host, &host); err != nil && err != badger.ErrKeyNotFound {
					return err
				}
				available = s.params.MaxPending - pending
				hosts[e.host] = s.params.MaxPendingPerOrigin - host
				return nil
			}); err != nil {
				return consumed, err
			}
		} else {
			return consumed, result
		}
	}
	if err := s.checkpointDiscovery(ctx, completed, cursor, last, deferrals, attempted[:]...); err != nil {
		return consumed, err
	}
	return consumed, nil
}

type discoveryEntry struct {
	noSlot       bool
	stale        bool
	route        string
	routeClass   int
	repeated     bool
	key          string
	request      message.FetcherRequest
	host         string
	rejected     bool
	excluded     bool
	parked       bool
	purgeReceipt bool
}

// Admission and staging cleanup deliberately remain separate transactions.
// Interrupted cleanup replays through idempotent admission. Each cleanup chunk
// is atomic, including its marker counts; a deleted cursor remains seekable.
func (s *Store) checkpointDiscovery(ctx context.Context, entries []discoveryEntry, previous, last string, deferrals int, attempted ...discoveryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rotation := int64(deferrals)
	for _, entry := range entries {
		if entry.stale || entry.excluded || entry.rejected || entry.parked || entry.noSlot {
			rotation++
		}
	}
	checkpoint := func(tx *badger.Txn) error {
		for _, e := range attempted {
			if err := s.checkpointRepeatedRoute(tx, e); err != nil {
				return err
			}
		}
		var current string
		if err := read(tx, "meta/staged_cursor", &current); err != nil && err != badger.ErrKeyNotFound {
			return err
		}
		// An overlapping pass must not rewind another caller's cursor.
		// Equality also permits normal wrap-around.
		if current == previous && last != "" {
			if err := write(tx, "meta/staged_cursor", last); err != nil {
				return err
			}
		}
		if s.hasRepeated.Load() && rotation > 0 {
			if err := increment(tx, "repeated/stage_deferral_step", rotation); err != nil {
				return err
			}
		}
		if deferrals > 0 {
			return increment(tx, "meta/capacity_deferrals", int64(deferrals))
		}
		return nil
	}
	size := 128
	for at := 0; at < len(entries); {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := size
		if n > len(entries)-at {
			n = len(entries) - at
		}
		err := s.update(func(tx *badger.Txn) error {
			skipped := int64(0)
			routing := &repeatedAcks{chunks: map[string]repeatedChunk{}}
			for _, e := range entries[at : at+n] {
				counted, err := s.ackDiscovery(tx, e, routing)
				if err != nil {
					return err
				}
				if counted {
					skipped++
				}
			}
			if err := s.flushRepeatedAcks(tx, routing); err != nil {
				return err
			}
			if skipped > 0 {
				if err := increment(tx, "meta/policy_skips", skipped); err != nil {
					return err
				}
			}
			if at+n == len(entries) {
				return checkpoint(tx)
			}
			return nil
		})
		if errors.Is(err, badger.ErrTxnTooBig) && n > 1 {
			size = n / 2
			continue
		}
		if err != nil {
			return err
		}
		at += n
	}
	if len(entries) > 0 || last == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.update(checkpoint)
}

func (s *Store) ackDiscovery(tx *badger.Txn, e discoveryEntry, caches ...*repeatedAcks) (bool, error) {
	if _, err := tx.Get([]byte(e.key)); err == badger.ErrKeyNotFound {
		return false, s.ackRepeatedChunk(tx, e.key, caches...)
	} else if err != nil {
		return false, err
	}
	if e.parked {
		if err := s.ackRepeatedChunk(tx, e.key, caches...); err != nil {
			return false, err
		}
		key := "trap/staged/" + util.SiteKey(e.request.URI) + "/" + e.key
		if err := write(tx, key, e.request); err != nil {
			return false, err
		}
		if err := tx.Delete([]byte(e.key)); err != nil {
			return false, err
		}
		if err := increment(tx, "meta/trap_parked", 1); err != nil {
			return false, err
		}
		return false, increment(tx, "meta/staged", -1)
	}
	if e.rejected {
		if err := increment(tx, "meta/budget_rejected", 1); err != nil {
			return false, err
		}
	}
	if err := tx.Delete([]byte(e.key)); err != nil {
		return false, err
	}
	if err := s.finishDiscoveryReceipt(tx, e.key, e.purgeReceipt, caches...); err != nil {
		return false, err
	}
	return e.excluded, increment(tx, "meta/staged", -1)
}

func (s *Store) finishDiscoveryReceipt(tx *badger.Txn, key string, deleteEmpty bool, caches ...*repeatedAcks) error {
	if err := s.ackRepeatedChunk(tx, key, caches...); err != nil {
		return err
	}
	parts := strings.Split(key, "/")
	if len(parts) != 3 || parts[0] != "staged" || parts[1] == "" {
		return fmt.Errorf("invalid discovery key")
	}
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
		if deleteEmpty {
			return tx.Delete([]byte(marker))
		}
		if err := writeTTL(tx, marker, 0, time.Duration(s.params.Retention.DedupFor)); err != nil {
			return err
		}
	} else if err := write(tx, marker, remaining); err != nil {
		return err
	}
	return nil
}
