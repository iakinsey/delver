package frontier

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/util"
)

func (s *Store) ScanCandidates(ctx context.Context, strategy string, cursor ScanCursor, limit int) (CandidatePage, error) {
	if !s.hasRepeated.Load() {
		return s.scanLegacyCandidates(ctx, strategy, cursor, limit)
	}
	page := CandidatePage{Cursor: cursor}
	if limit < 1 {
		return page, fmt.Errorf("candidate scan limit must be positive")
	}
	err := s.view(func(tx *badger.Txn) error {
		prefix := "b/"
		diverse := strategy == "host_diverse" || strategy == "adaptive"
		if strategy == "depth_first" {
			prefix = "d/"
		} else if diverse {
			prefix = "hs/"
		}
		repeatedPrefix := "r" + prefix
		if s.params.Retry != nil {
			prefix = "f" + prefix
			repeatedPrefix = "f" + repeatedPrefix
		}
		prefixes := [4]string{"reserved/", "retry_ready/", prefix, repeatedPrefix}
		after := [4]string{cursor.After[0], cursor.After[1], cursor.After[2], cursor.RepeatedAfter}
		done := [4]bool{cursor.Done[0], cursor.Done[1], cursor.Done[2], cursor.RepeatedDone}
		defer func() {
			copy(page.Cursor.After[:], after[:3])
			copy(page.Cursor.Done[:], done[:3])
			page.Cursor.RepeatedAfter = after[3]
			page.Cursor.RepeatedDone = done[3]
		}()
		if s.params.Retry == nil {
			done[1] = true
		}
		var state RepeatedRouting
		if e := repeatedRead(tx, "repeated/routing", &state); e != nil {
			return e
		}
		active := s.repeatedSettings().Mode == "deprioritize" && state.Ready
		seen := map[string]bool{}
		cooling := map[string]bool{}
		now := time.Now().UnixNano()
		its := [4]*badger.Iterator{}
		get := func(stream int) *badger.Iterator {
			if its[stream] != nil {
				return its[stream]
			}
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			it := tx.NewIterator(opts)
			its[stream] = it
			start := prefixes[stream]
			if after[stream] != "" {
				start = after[stream] + "\x00"
			}
			it.Seek([]byte(start))
			return it
		}
		defer func() {
			for _, it := range its {
				if it != nil {
					it.Close()
				}
			}
		}()
		valid := func(stream int) bool {
			if done[stream] {
				return false
			}
			if !get(stream).ValidForPrefix([]byte(prefixes[stream])) {
				done[stream] = true
				return false
			}
			return true
		}
		visit := func(stream int) error {
			it := get(stream)
			key := string(it.Item().Key())
			after[stream] = key
			page.Inspected++
			var uri string
			if e := read(tx, key, &uri); e != nil {
				return e
			}
			var c Candidate
			if e := read(tx, "u/"+uri, &c); e == badger.ErrKeyNotFound {
				it.Next()
				return nil
			} else if e != nil {
				return e
			}
			eligible := stream == 0 && c.State == "reserved" || stream != 0 && c.State == "pending"
			if !seen[uri] && eligible {
				seen[uri] = true
				if s.trapBlocked(tx, c.Request.URI) {
					page.Trapped = append(page.Trapped, c)
				} else if util.URLFileExcluded(c.Request.URI, s.params.ExcludedExtensions) {
					page.Excluded = append(page.Excluded, c)
				} else {
					blocked := false
					if stream != 0 {
						var known bool
						blocked, known = cooling[c.Request.Host]
						if !known {
							var next int64
							if e := repeatedRead(tx, "cool/"+c.Request.Host, &next); e != nil {
								return e
							}
							blocked = next > now
							cooling[c.Request.Host] = blocked
						}
					}
					if blocked {
						page.Cooling++
					} else {
						if !active {
							c.Repeated = false
						}
						page.Candidates = append(page.Candidates, c)
						if diverse && stream >= 2 {
							parts := strings.SplitN(strings.TrimPrefix(key, prefixes[stream]), "/", 2)
							after[stream] = prefixes[stream] + parts[0] + "/\xff"
							it.Seek([]byte(after[stream]))
							return nil
						}
					}
				}
			}
			it.Next()
			return nil
		}
		scan := func(stream, budget int) error {
			for n := 0; n < budget && valid(stream); n++ {
				if e := ctx.Err(); e != nil {
					return e
				}
				if e := visit(stream); e != nil {
					return e
				}
			}
			return nil
		}
		reserved := max(1, limit/4)
		if e := scan(0, reserved); e != nil {
			return e
		}
		retry := 0
		if s.params.Retry != nil {
			retry = max(1, int(float64(limit)*s.params.Retry.Share))
		}
		if e := scan(1, min(retry, limit-page.Inspected)); e != nil {
			return e
		}
		for page.Inspected < limit {
			if e := ctx.Err(); e != nil {
				return e
			}
			normal, repeated := valid(2), valid(3)
			if !normal && !repeated {
				break
			}
			stream := 2
			if !normal {
				stream = 3
			} else if repeated {
				if active {
					if repeatedTurn(s.repeatedScanStep.Add(1)-1, s.repeatedSettings().AdmissionShare) {
						stream = 3
					}
				} else {
					// Merge by the original strategy key while observing or restoring.
					a := strings.TrimPrefix(string(get(2).Item().Key()), prefix)
					b := strings.TrimPrefix(string(get(3).Item().Key()), repeatedPrefix)
					if b < a {
						stream = 3
					}
				}
			}
			if e := visit(stream); e != nil {
				return e
			}
		}
		if !valid(2) && !valid(3) {
			if e := scan(0, limit-page.Inspected); e != nil {
				return e
			}
			if e := scan(1, limit-page.Inspected); e != nil {
				return e
			}
		}
		for i := range done {
			valid(i)
		}
		page.Complete = done[0] && done[1] && done[2] && done[3]
		return nil
	})
	return page, err
}
