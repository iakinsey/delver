package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/protection"
	"io"
	"os"
	"path/filepath"
)

func verifyPayload(raw []byte, match func(string) bool) error {
	if !json.Valid(raw) {
		return nil
	}
	_, changed, e := protection.SanitizeJSON(raw, match)
	if e != nil {
		return e
	}
	if changed {
		return fmt.Errorf("matching queue data remains")
	}
	return nil
}
func (s *fileQueue) VerifyNoMatches(ctx context.Context, match func(string) bool) error {
	s.putLock.Lock()
	defer s.putLock.Unlock()
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
			entries, readErr := dir.ReadDir(128)
			for _, entry := range entries {
				if e = ctx.Err(); e != nil {
					dir.Close()
					return e
				}
				if entry.IsDir() || entry.Name() == ".lock" {
					continue
				}
				if entry.Type()&os.ModeType != 0 {
					dir.Close()
					return fmt.Errorf("unsupported queue file prevents verification")
				}
				raw, x := os.ReadFile(filepath.Join(root, entry.Name()))
				if os.IsNotExist(x) {
					continue
				}
				if x != nil {
					dir.Close()
					return x
				}
				if !json.Valid(raw) {
					dir.Close()
					return fmt.Errorf("invalid queue record prevents verification")
				}
				if e = verifyPayload(raw, match); e != nil {
					dir.Close()
					return e
				}
			}
			if readErr != nil {
				dir.Close()
				if readErr != io.EOF {
					return readErr
				}
				break
			}
		}
	}
	return nil
}
func (s *crawlQueue) VerifyNoMatches(ctx context.Context, match func(string) bool) error {
	return s.db.View(func(tx *badger.Txn) error {
		it := tx.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			if e := ctx.Err(); e != nil {
				return e
			}
			if e := it.Item().Value(func(raw []byte) error { return verifyPayload(raw, match) }); e != nil {
				return e
			}
		}
		return nil
	})
}
