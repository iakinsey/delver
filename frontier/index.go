package frontier

import (
	"context"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/util"
	"strings"
)

func schedulingKeys(c Candidate) []string {
	keys := []string{
		fmt.Sprintf("b/%010d/%020d/%s", c.Request.Depth, c.Sequence, c.Request.URI),
		fmt.Sprintf("d/%010d/%020d/%s", int64(2147483647)-int64(c.Request.Depth), int64(9223372036854775807)-c.Sequence, c.Request.URI),
		fmt.Sprintf("hs/%s/%010d/%020d/%s", util.SiteKey(c.Request.Host), c.Request.Depth, c.Sequence, c.Request.URI),
	}
	if c.Repeated {
		for i, key := range keys {
			keys[i] = "r" + key
		}
	}
	if c.Request.Attempt > 0 {
		keys = append(keys, fmt.Sprintf("retry_ready/%020d/%s", c.Sequence, c.Request.URI))
	} else {
		for _, key := range append([]string{}, keys...) {
			keys = append(keys, "f"+key)
		}
	}
	return keys
}
func schedule(tx *badger.Txn, c Candidate, remove bool) error {
	for _, k := range schedulingKeys(c) {
		var err error
		if remove {
			err = tx.Delete([]byte(k))
		} else {
			err = write(tx, k, c.Request.URI)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Candidates uses strategy-specific indexes; completed history is never visited.
func (s *Store) Candidates(ctx context.Context, strategy, lastHost string, limit int) ([]Candidate, error) {
	if s.hasRepeated.Load() {
		page, err := s.ScanCandidates(ctx, strategy, ScanCursor{}, limit)
		return page.Candidates, err
	}
	out := []Candidate{}
	err := s.view(func(tx *badger.Txn) error {
		seen := map[string]bool{}
		ropts := badger.DefaultIteratorOptions
		ropts.PrefetchValues = false
		reserved := tx.NewIterator(ropts)
		for reserved.Seek([]byte("reserved/")); reserved.ValidForPrefix([]byte("reserved/")) && len(out) < limit; reserved.Next() {
			var uri string
			if err := read(tx, string(reserved.Item().Key()), &uri); err != nil {
				reserved.Close()

				return err
			}
			var c Candidate
			if err := read(tx, "u/"+uri, &c); err != nil {
				reserved.Close()
				return err
			}
			out = append(out, c)
			seen[uri] = true
		}
		reserved.Close()
		// Reserve a bounded part of the candidate window for eligible retries so
		// a large untouched seed index cannot starve their recovery probes.
		if s.params.Retry != nil {
			quota := int(float64(limit) * s.params.Retry.Share)
			if quota < 1 {
				quota = 1
			}
			retry := tx.NewIterator(badger.DefaultIteratorOptions)
			n := 0
			for retry.Seek([]byte("retry_ready/")); retry.ValidForPrefix([]byte("retry_ready/")) && n < quota && len(out) < limit; retry.Next() {
				if err := ctx.Err(); err != nil {
					retry.Close()
					return err
				}
				var uri string
				if err := read(tx, string(retry.Item().Key()), &uri); err != nil {
					retry.Close()
					return err
				}
				if seen[uri] {
					continue
				}
				var c Candidate
				if err := read(tx, "u/"+uri, &c); err != nil {
					retry.Close()
					return err
				}
				out = append(out, c)
				seen[uri] = true
				n++
			}
			retry.Close()
		}

		prefix := "b/"
		if strategy == "depth_first" {
			prefix = "d/"
		}
		diverse := strategy == "host_diverse" || strategy == "adaptive"
		if diverse {
			prefix = "hs/"
		}
		if s.params.Retry != nil {
			prefix = "f" + prefix
		}
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := tx.NewIterator(opts)
		defer it.Close()
		scan := func(start, stop string) error {
			for it.Seek([]byte(start)); it.ValidForPrefix([]byte(prefix)) && len(out) < limit; {
				if err := ctx.Err(); err != nil {
					return err
				}
				key := string(it.Item().Key())
				if stop != "" && key >= stop {
					break
				}
				var uri string
				if err := read(tx, key, &uri); err != nil {
					return err
				}
				var c Candidate
				if err := read(tx, "u/"+uri, &c); err != nil {
					return err
				}
				if !seen[uri] {
					out = append(out, c)
					seen[uri] = true
				}
				if diverse {
					parts := strings.SplitN(key, "/", 3)
					it.Seek([]byte(prefix + parts[1] + "/\xff"))
				} else {
					it.Next()
				}
			}
			return nil
		}
		start := prefix
		if diverse && lastHost != "" {
			start = prefix + util.SiteKey(lastHost) + "/\xff"
		}
		if err := scan(start, ""); err != nil {
			return err
		}
		if diverse && start != prefix && len(out) < limit {
			if err := scan(prefix, start); err != nil {
				return err
			}
		}
		// Fill unused fresh capacity with retries when no fresh candidates remain.
		if s.params.Retry != nil && len(out) < limit {
			retry := tx.NewIterator(badger.DefaultIteratorOptions)
			defer retry.Close()
			for retry.Seek([]byte("retry_ready/")); retry.ValidForPrefix([]byte("retry_ready/")) && len(out) < limit; retry.Next() {
				if err := ctx.Err(); err != nil {
					return err
				}
				var uri string
				if err := read(tx, string(retry.Item().Key()), &uri); err != nil {
					return err
				}
				if seen[uri] {
					continue
				}
				var c Candidate
				if err := read(tx, "u/"+uri, &c); err != nil {
					return err
				}
				out = append(out, c)
				seen[uri] = true
			}
		}
		return nil
	})
	return out, err
}
