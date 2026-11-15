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
		if _, err := txn.Get([]byte("active_ready\x00" + s.params.CrawlID)); err == nil {
			for state := range counts {
				item, err := txn.Get([]byte("count\x00" + s.params.CrawlID + "\x00" + state))
				if err != nil {
					return err
				}
				var n int64
				if err := item.Value(func(b []byte) error { return json.Unmarshal(b, &n) }); err != nil {
					return err
				}
				counts[state] = n
			}
			return nil
		} else if err != badger.ErrKeyNotFound {
			return err
		}

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
