package frontier

import (
	"context"
	"errors"

	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/util"
)

// Try the common, unconstrained path as one transaction. Any rejection rolls
// back the whole batch, then the existing per-record path handles that window.
// This preserves rejection semantics even when admitTx wrote before rejecting.
func (s *Store) batchAdmissions(ctx context.Context, entries []discoveryEntry, hosts map[string]int, available int, known map[string]bool) (map[string]bool, error) {
	if len(entries) < 2 {
		return nil, nil
	}
	remaining := map[string]int{}
	for k, v := range hosts {
		remaining[k] = v
	}
	uris := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.stale {
			return nil, nil
		}
		if e.request.Depth < 0 || e.request.Hops < 0 {
			return nil, nil
		}
		uri, err := Normalize(e.request.URI, "")
		if err != nil || uri != e.request.URI {
			return nil, nil
		}
		if err := s.policy.Check(e.request.URI); err != nil {
			return nil, nil
		}
		if util.URLFileExcluded(e.request.URI, s.params.ExcludedExtensions) {
			return nil, nil
		}
		if !known[e.request.URI] {
			if available <= 0 || remaining[e.host] <= 0 {
				return nil, nil
			}
			available--
			remaining[e.host]--
		}
		uris = append(uris, e.request.URI)
	}
	results := map[string]bool{}
	err := s.policy.GuardMany(uris, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.checkDisk(); err != nil {
			return err
		}
		return s.update(func(tx *badger.Txn) error {
			cache := newRepeatedClassifier(s, tx)
			for _, e := range entries {
				if err := ctx.Err(); err != nil {
					return err
				}
				used := false
				if err := s.admitTx(tx, e.request, &used, cache); err != nil {
					return err
				}
				results[e.key] = used
			}
			return nil
		})
	})
	if err != nil {
		if errors.Is(err, ErrCapacity) || errors.Is(err, ErrRejected) || errors.Is(err, ErrTrapExcluded) || errors.Is(err, badger.ErrTxnTooBig) {
			return nil, nil
		}
		// Policy changes are rechecked by the ordinary path.
		if errors.Is(err, protection.ErrBlocked) || errors.Is(err, protection.ErrChanged) {
			return nil, nil
		}
		return nil, err
	}
	return results, nil
}
