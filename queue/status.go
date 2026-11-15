package queue

import (
	"encoding/json"
	"github.com/dgraph-io/badger/v3"
	"os"
	"strings"
)

func (s *crawlQueue) Counts() (map[string]int64, error) {
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	if s.closed {
		return nil, badger.ErrDBClosed
	}
	counts := map[string]int64{"pending": 0, "leased": 0, "completed": 0, "failed": 0}
	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte(s.params.CrawlID + "\x00")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			var record crawlRecord
			if err := it.Item().Value(func(data []byte) error { return json.Unmarshal(data, &record) }); err != nil {
				return err
			}
			counts[record.State]++
		}
		return nil
	})
	return counts, err
}
func (s *fileQueue) Counts() (map[string]int64, error) {
	counts := map[string]int64{"pending": 0, "leased": 0, "failed": 0}
	for _, dir := range []string{s.path, s.dlqPath} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name := entry.Name()
			state := "pending"
			if strings.HasSuffix(name, claimedSuffix) {
				state = "leased"
				name = strings.TrimSuffix(name, claimedSuffix)
			}
			if !s.entityRegex.MatchString(name) {
				continue
			}
			if dir == s.dlqPath {
				state = "failed"
			}
			counts[state]++
		}
	}
	return counts, nil
}
