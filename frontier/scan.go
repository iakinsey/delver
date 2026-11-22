package frontier

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/util"
)

// ScanCursor is a transient position in the existing reserved, retry and fresh
// indexes. Keys remain valid seek positions after deletion. Nothing is persisted.
type ScanCursor struct {
	After [3]string
	Done  [3]bool
}
type CandidatePage struct {
	Trapped            []Candidate
	Excluded           []Candidate
	Candidates         []Candidate
	Cursor             ScanCursor
	Inspected, Cooling int
	Complete           bool
}

// ScanCandidates bounds inspected records, not just returned candidates. A caller
// continues a refill across passes and resets the cursor when the refill or sweep
// completes. Cooling entries cannot pin every pass to the same index prefix.
func (s *Store) ScanCandidates(ctx context.Context, strategy string, cursor ScanCursor, limit int) (CandidatePage, error) {
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
		if s.params.Retry != nil {
			prefix = "f" + prefix
		} else {
			page.Cursor.Done[1] = true
		}
		prefixes := [3]string{"reserved/", "retry_ready/", prefix}
		cooling := map[string]bool{}
		seen := map[string]bool{}
		now := time.Now().UnixNano()
		scan := func(stream, budget int) error {
			if page.Cursor.Done[stream] || budget <= 0 {
				return nil
			}
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			it := tx.NewIterator(opts)
			defer it.Close()
			start := prefixes[stream]
			if page.Cursor.After[stream] != "" {
				start = page.Cursor.After[stream]
			}
			it.Seek([]byte(start))
			if it.Valid() && string(it.Item().Key()) == page.Cursor.After[stream] {
				it.Next()
			}
			for n := 0; it.ValidForPrefix([]byte(prefixes[stream])) && n < budget; n++ {
				if err := ctx.Err(); err != nil {
					return err
				}
				key := string(it.Item().Key())
				page.Cursor.After[stream] = key
				page.Inspected++
				var uri string
				if err := read(tx, key, &uri); err != nil {
					return err
				}
				var c Candidate
				if err := read(tx, "u/"+uri, &c); err != nil {
					return err
				}
				if !seen[uri] && (stream == 0 && c.State == "reserved" || stream != 0 && c.State == "pending") {
					if s.trapBlocked(tx, c.Request.URI) {
						page.Trapped = append(page.Trapped, c)
						seen[uri] = true
						it.Next()
						continue
					}
					if util.URLFileExcluded(c.Request.URI, s.params.ExcludedExtensions) {
						page.Excluded = append(page.Excluded, c)
						seen[uri] = true
						it.Next()
						continue
					}
					blocked := false
					if stream != 0 {
						var ok bool
						blocked, ok = cooling[c.Request.Host]
						if !ok {
							var next int64
							if err := read(tx, "cool/"+c.Request.Host, &next); err != nil && err != badger.ErrKeyNotFound {
								return err
							}
							blocked = next > now
							cooling[c.Request.Host] = blocked
						}
					}
					if blocked {
						page.Cooling++
					} else {
						page.Candidates = append(page.Candidates, c)
						seen[uri] = true
						if diverse && stream == 2 {
							parts := strings.SplitN(key, "/", 3)
							page.Cursor.After[stream] = prefix + parts[1] + "/\xff"
							it.Seek([]byte(page.Cursor.After[stream]))
							continue
						}
					}
				}
				it.Next()
			}
			page.Cursor.Done[stream] = !it.ValidForPrefix([]byte(prefixes[stream]))
			return nil
		}
		// Recovery and retry work each receive a bounded share; unused capacity is
		// available to fresh work. All streams retain their own continuation.
		reserved := limit / 4
		if reserved < 1 {
			reserved = 1
		}
		if err := scan(0, reserved); err != nil {
			return err
		}
		retry := 0
		if s.params.Retry != nil {
			retry = int(float64(limit) * s.params.Retry.Share)
			if retry < 1 {
				retry = 1
			}
			if retry > limit-page.Inspected {
				retry = limit - page.Inspected
			}
		}
		if err := scan(1, retry); err != nil {
			return err
		}
		if err := scan(2, limit-page.Inspected); err != nil {
			return err
		}
		// When fresh work is exhausted, lend its remaining budget to recovery/retries.
		if page.Cursor.Done[2] {
			if err := scan(0, limit-page.Inspected); err != nil {
				return err
			}
			if err := scan(1, limit-page.Inspected); err != nil {
				return err
			}
		}
		page.Complete = page.Cursor.Done[0] && page.Cursor.Done[1] && page.Cursor.Done[2]
		return nil
	})
	return page, err
}
