package queue

import (
	"encoding/json"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/types/message"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Pressure reports only live queue work, never a scan through completed history.
func (s *crawlQueue) Pressure() (map[string]int64, float64, error) {
	counts, err := s.Counts()
	if err != nil {
		return nil, 0, err
	}
	oldest := time.Now().UnixNano()
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	if s.closed {
		return nil, 0, badger.ErrDBClosed
	}
	err = s.db.View(func(tx *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := tx.NewIterator(opts)
		defer it.Close()
		prefix := []byte("active\x00" + s.params.CrawlID + "\x00")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			key := it.Item().KeyCopy(nil)[len("active\x00"):]
			item, e := tx.Get(key)
			if e != nil {
				return e
			}
			var r crawlRecord
			if e = item.Value(func(b []byte) error { return json.Unmarshal(b, &r) }); e != nil {
				return e
			}
			var request message.FetcherRequest
			if e = json.Unmarshal(r.Message.Message, &request); e != nil {
				return e
			}
			if request.QueuedAt > 0 && request.QueuedAt < oldest {
				oldest = request.QueuedAt
			}
		}
		return nil
	})
	return counts, time.Since(time.Unix(0, oldest)).Seconds(), err
}
func (s *fileQueue) Pressure() (map[string]int64, float64, error) {
	counts, err := s.Counts()
	if err != nil {
		return nil, 0, err
	}
	entries, err := os.ReadDir(s.path)
	if err != nil {
		return nil, 0, err
	}
	oldest := time.Now()
	for _, e := range entries {
		if !s.entityRegex.MatchString(strings.TrimSuffix(e.Name(), claimedSuffix)) {
			continue
		}
		info, err := os.Stat(filepath.Join(s.path, e.Name()))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, 0, err
		}
		if info.ModTime().Before(oldest) {
			oldest = info.ModTime()
		}
	}
	return counts, time.Since(oldest).Seconds(), nil
}
