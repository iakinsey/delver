package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/types/message"
	"os"
	"path/filepath"
	"strings"

	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/types"
)

type FailedRecord struct {
	Error     string `json:"error,omitempty"`
	ID        string `json:"id"`
	MessageID string `json:"message_id"`
	Attempts  int    `json:"attempts"`
	State     string `json:"state"`
}
type Recovery interface {
	Failed(int, int) ([]FailedRecord, error)
	Recover(string, bool) error
}

func (s *crawlQueue) Failed(offset, limit int) ([]FailedRecord, error) {
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	if s.closed {
		return nil, badger.ErrDBClosed
	}
	records := []FailedRecord{}
	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte(s.params.CrawlID + "\x00")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			var record crawlRecord
			if err := it.Item().Value(func(data []byte) error { return json.Unmarshal(data, &record) }); err != nil {
				return err
			}
			if record.State != "failed" {
				continue
			}
			if offset > 0 {
				offset--
				continue
			}
			records = append(records, FailedRecord{Error: record.Message.LastError, ID: string(it.Item().Key()[len(prefix):]), MessageID: record.Message.ID, Attempts: record.Attempts, State: record.State})
			if len(records) >= limit {
				break
			}
		}
		return nil
	})
	return records, err
}
func (s *crawlQueue) Recover(id string, recrawl bool) error {
	key := []byte(s.params.CrawlID + "\x00" + id)
	return s.update(context.Background(), func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		var record crawlRecord
		if err = item.Value(func(data []byte) error { return json.Unmarshal(data, &record) }); err != nil {
			return err
		}
		actual, err := s.key(record.Message)
		if err != nil {
			return err
		}
		if string(actual) != string(key) {
			return fmt.Errorf("crawl record URI does not match its key")
		}
		if staged, e := txn.Get(append([]byte("outputs\x00"), key...)); e == nil {
			if e = staged.Value(func(data []byte) error { var outputs []types.Message; return json.Unmarshal(data, &outputs) }); e != nil {
				return e
			}
		} else if e != badger.ErrKeyNotFound {
			return e
		}
		expected := "failed"
		if recrawl {
			expected = "completed"
		}
		if record.State == "pending" || record.State == "leased" {
			return nil
		}
		if record.State != expected {
			return fmt.Errorf("record must be %s", expected)
		}
		record.State = "pending"
		record.Attempts = 0
		record.Message.Attempts = 0
		record.Message.LastError = ""
		if recrawl {
			record.Message.ID = string(types.NewV4())
			var request message.FetcherRequest
			if err := json.Unmarshal(record.Message.Message, &request); err != nil {
				return err
			}
			request.RequestID = types.NewV4()
			payload, err := json.Marshal(request)
			if err != nil {
				return err
			}
			record.Message.Message = payload
			if err = txn.Delete(append([]byte("outputs\x00"), key...)); err != nil {
				return err
			}
		}
		data, err := json.Marshal(record)
		if err != nil {
			return err
		}
		return s.setRecord(txn, key, data)
	})
}
func (s *fileQueue) Failed(offset, limit int) ([]FailedRecord, error) {
	s.putLock.Lock()
	defer s.putLock.Unlock()
	files, err := os.ReadDir(s.dlqPath)
	if err != nil {
		return nil, err
	}
	records := []FailedRecord{}
	for _, file := range files {
		if !s.entityRegex.MatchString(file.Name()) {
			continue
		}
		if offset > 0 {
			offset--
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dlqPath, file.Name()))
		if err != nil {
			return nil, err
		}
		var msg types.Message
		if err = json.Unmarshal(data, &msg); err != nil {
			records = append(records, FailedRecord{ID: file.Name(), State: "failed", Error: "Corrupt message envelope; repair before replay"})
			if len(records) >= limit {
				break
			}
			continue
		}
		records = append(records, FailedRecord{Error: msg.LastError, ID: file.Name(), MessageID: msg.ID, Attempts: msg.Attempts, State: "failed"})
		if len(records) >= limit {
			break
		}
	}
	return records, nil
}

type fileReplay struct {
	Message types.Message   `json:"message"`
	Outputs json.RawMessage `json:"outputs,omitempty"`
}

func (s *fileQueue) Recover(id string, recrawl bool) error {
	if recrawl {
		return fmt.Errorf("recrawl requires a crawl queue")
	}
	if !s.entityRegex.MatchString(id) || strings.ContainsAny(id, "/\\") {
		return fmt.Errorf("invalid record id")
	}
	s.putLock.Lock()
	defer s.putLock.Unlock()
	manifest := filepath.Join(s.dlqPath, id) + ".replay"
	if _, err := os.Stat(manifest); err == nil {
		return s.finishReplay(id)
	} else if !os.IsNotExist(err) {
		return err
	}
	source := filepath.Join(s.dlqPath, id)
	data, err := os.ReadFile(source)
	if os.IsNotExist(err) {
		if _, e := os.Stat(source + ".replayed"); e == nil {
			return nil
		}
	}
	if err != nil {
		return err
	}
	var replay fileReplay
	if err = json.Unmarshal(data, &replay.Message); err != nil {
		return err
	}
	replay.Message.Attempts = 0
	replay.Message.LastError = ""
	if staged, e := os.ReadFile(source + claimedSuffix + ".outputs"); e == nil {
		var outputs []types.Message
		if e = json.Unmarshal(staged, &outputs); e != nil {
			return e
		}
		replay.Outputs = staged
	} else if !os.IsNotExist(e) {
		return e
	}
	data, err = json.Marshal(replay)
	if err != nil {
		return err
	}
	if err = writeRecovery(manifest, data); err != nil {
		return err
	}
	return s.finishReplay(id)
}
func (s *fileQueue) finishReplay(id string) error {
	source := filepath.Join(s.dlqPath, id)
	target := filepath.Join(s.path, id)
	manifest := source + ".replay"
	data, err := os.ReadFile(manifest)
	if err != nil {
		return err
	}
	var replay fileReplay
	if err = json.Unmarshal(data, &replay); err != nil {
		return err
	}
	if replay.Message.ID == "" {
		return fmt.Errorf("replay message id is required")
	}
	if len(replay.Outputs) > 0 {
		var outputs []types.Message
		if err = json.Unmarshal(replay.Outputs, &outputs); err != nil {
			return err
		}
	}
	// The queue pump shares putLock, so it cannot claim a partially restored batch.
	if len(replay.Outputs) > 0 {
		if err = writeRecovery(target+claimedSuffix+".outputs", replay.Outputs); err != nil {
			return err
		}
	}
	messageData, err := json.Marshal(replay.Message)
	if err != nil {
		return err
	}
	if err = writeRecovery(target, messageData); err != nil {
		return err
	}
	if err = writeRecovery(source+".replayed", []byte(`{}`)); err != nil {
		return err
	}
	for _, path := range []string{source, source + claimedSuffix + ".outputs", manifest} {
		if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	dir, err := os.Open(s.dlqPath)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (s *fileQueue) restoreReplays() error {
	files, err := os.ReadDir(s.dlqPath)
	if err != nil {
		return err
	}
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".replay") {
			id := strings.TrimSuffix(file.Name(), ".replay")
			if !s.entityRegex.MatchString(id) {
				return fmt.Errorf("invalid replay record")
			}
			if err = s.finishReplay(id); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeRecovery(path string, data []byte) error {
	temporary := path + writingSuffix
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temporary, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
