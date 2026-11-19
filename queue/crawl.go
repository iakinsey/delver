package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"
)

type CrawlQueueParams struct {
	AcceptFrontierGenerations bool          `json:"accept_frontier_generations"`
	NotifyDispatch            bool          `json:"notify_dispatch"`
	Path                      string        `json:"path"`
	CrawlID                   string        `json:"crawl_id"`
	MaxAttempts               int           `json:"max_attempts"`
	RecrawlAfter              time.Duration `json:"recrawl_after"`
}
type crawlRecord struct {
	Message  types.Message
	State    string
	Updated  int64
	Attempts int
}
type crawlQueue struct {
	admission
	db                             *badger.DB
	params                         CrawlQueueParams
	channel                        chan types.Message
	wake                           chan struct{}
	done                           chan struct{}
	stopped                        chan struct{}
	startOnce, stopOnce, closeOnce sync.Once
	dbMu                           sync.RWMutex
	writeMu                        sync.Mutex
	closed                         bool
	healthMu                       sync.Mutex
	lastError                      string
	frontierIdentity               func(string) (message.FetcherRequest, bool, error)
}

// SetFrontierIdentityLookup is wired before Start by the frontier publisher.
// Prepare uses its existing recovery scan to repair only confirmed old identity
// mismatches; valid retries may legitimately use different generation/request IDs.
func (s *crawlQueue) SetFrontierIdentityLookup(lookup func(string) (message.FetcherRequest, bool, error)) {
	s.frontierIdentity = lookup
}

func NewCrawlQueue(p CrawlQueueParams) (Queue, error) {
	if p.Path == "" || p.CrawlID == "" || p.MaxAttempts < 1 {
		return nil, fmt.Errorf("crawl queue requires path, crawl_id and positive max_attempts")
	}
	db, err := badger.Open(badger.DefaultOptions(p.Path).WithLogger(nil).WithSyncWrites(true))
	if err != nil {
		return nil, err
	}
	return &crawlQueue{wake: make(chan struct{}, 1), db: db, params: p, channel: make(chan types.Message), done: make(chan struct{}), stopped: make(chan struct{})}, nil
}
func crawlURI(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid crawl URI %q", raw)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}
func (s *crawlQueue) key(msg types.Message) ([]byte, error) {
	var req message.FetcherRequest
	if msg.MessageType != types.FetcherRequestType {
		return nil, fmt.Errorf("crawl queue accepts fetcher requests only")
	}
	if err := json.Unmarshal(msg.Message, &req); err != nil {
		return nil, err
	}
	uri, err := crawlURI(req.URI)
	if err != nil {
		return nil, err
	}
	return []byte(s.params.CrawlID + "\x00" + uri), nil
}
func (s *crawlQueue) Put(msg types.Message, priority int) error {
	return s.PutContext(context.Background(), msg, priority)
}
func (s *crawlQueue) PutContext(ctx context.Context, msg types.Message, priority int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-s.done:
		return fmt.Errorf("crawl queue stopped")
	default:
	}
	key, err := s.key(msg)
	if err != nil {
		return err
	}
	var normalized message.FetcherRequest
	json.Unmarshal(msg.Message, &normalized)
	normalized.URI, _ = crawlURI(normalized.URI)
	parsed, _ := url.Parse(normalized.URI)
	normalized.Host = parsed.Host
	normalized.Protocol = types.ProtocolHTTP
	if normalized.RequestID == "" {
		normalized.RequestID = types.NewV4()
	}
	msg.Message, err = json.Marshal(normalized)
	if err != nil {
		return err
	}
	if msg.ID == "" {
		msg.ID = string(types.NewV4())
	}
	return s.update(ctx, func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err == nil {
			var old crawlRecord
			if err = item.Value(func(data []byte) error { return json.Unmarshal(data, &old) }); err != nil {
				return err
			}
			var incoming, existing message.FetcherRequest
			if err := json.Unmarshal(msg.Message, &incoming); err != nil {
				return err
			}
			if err := json.Unmarshal(old.Message.Message, &existing); err != nil {
				return err
			}
			newGeneration := s.params.AcceptFrontierGenerations && incoming.FrontierGeneration != "" && incoming.FrontierGeneration != existing.FrontierGeneration
			if newGeneration && old.State == "leased" {
				return fmt.Errorf("previous frontier generation is still leased")
			}
			// A new generation owns its request identity, even when its path is
			// better. Only an update within the same generation retains the ID.
			if !newGeneration && incoming.BetterTraversalThan(existing) && old.State != "failed" {
				incoming.RequestID = existing.RequestID
				data, err := json.Marshal(incoming)
				if err != nil {
					return err
				}
				old.Message.Message = data
				if old.State == "completed" {
					old.State = "pending"
				}
				if old.State != "leased" {
					if err := txn.Delete(append([]byte("outputs\x00"), key...)); err != nil {
						return err
					}
				}
				data, err = json.Marshal(old)
				if err != nil {
					return err
				}
				return s.setRecord(txn, key, data)
			}
			if old.State != "completed" || (!newGeneration && (s.params.RecrawlAfter <= 0 || time.Since(time.Unix(0, old.Updated)) < s.params.RecrawlAfter)) {
				return nil
			}
		} else if err != badger.ErrKeyNotFound {
			return err
		}
		data, err := json.Marshal(crawlRecord{Message: msg, State: "pending", Updated: time.Now().UnixNano()})
		if err != nil {
			return err
		}
		return s.setRecord(txn, key, data)
	})
}
func (s *crawlQueue) Prepare() error {
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	if s.closed {
		return badger.ErrDBClosed
	}
	prefix := []byte(s.params.CrawlID + "\x00")
	if err := s.db.DropPrefix(append([]byte("pending\x00"), prefix...)); err != nil {
		return err
	}
	if err := s.db.DropPrefix([]byte("active\x00" + s.params.CrawlID + "\x00")); err != nil {
		return err
	}
	counts := map[string]int64{"pending": 0, "leased": 0, "completed": 0, "failed": 0}
	batch := s.db.NewWriteBatch()
	defer batch.Cancel()
	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			var record crawlRecord
			if err := item.Value(func(data []byte) error { return json.Unmarshal(data, &record) }); err != nil {
				return err
			}
			changed := false
			if record.State == "leased" {
				record.State = "pending"
				changed = true
			}
			if s.params.AcceptFrontierGenerations && s.frontierIdentity != nil && record.State != "failed" {
				var queued message.FetcherRequest
				if err := json.Unmarshal(record.Message.Message, &queued); err != nil {
					return err
				}
				if queued.FrontierGeneration != "" && queued.FrontierGeneration != queued.RequestID {
					current, found, err := s.frontierIdentity(queued.URI)
					if err != nil {
						return err
					}
					if found && current.URI == queued.URI && current.RequestID != "" && current.FrontierGeneration == queued.FrontierGeneration && current.RequestID != queued.RequestID {
						current.QueuedAt = time.Now().UnixNano()
						record.Message.Message, err = json.Marshal(current)
						if err != nil {
							return err
						}
						record.Message.ID = string(current.RequestID)
						record.State, record.Attempts, record.Updated = "pending", 0, time.Now().UnixNano()
						if err := batch.Delete(append([]byte("outputs\x00"), item.KeyCopy(nil)...)); err != nil {
							return err
						}
						changed = true
					}
				}
			}
			if changed {
				data, err := json.Marshal(record)
				if err != nil {
					return err
				}
				if err = batch.Set(item.KeyCopy(nil), data); err != nil {
					return err
				}
			}
			counts[record.State]++
			if record.State == "pending" {
				if err := batch.Set(append([]byte("active\x00"), item.KeyCopy(nil)...), nil); err != nil {
					return err
				}
				if err := batch.Set(s.pendingKey(item.KeyCopy(nil)), nil); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for state, count := range counts {
		data, err := json.Marshal(count)
		if err != nil {
			return err
		}
		if err := batch.Set([]byte("count\x00"+s.params.CrawlID+"\x00"+state), data); err != nil {
			return err
		}
	}
	if err := batch.Set([]byte("active_ready\x00"+s.params.CrawlID), []byte{1}); err != nil {
		return err
	}
	return batch.Flush()
}

func (s *crawlQueue) Start() error {
	var err error
	s.startOnce.Do(func() {
		if err = s.Prepare(); err != nil {
			close(s.stopped)
			return
		}
		go s.perform()
	})
	return err
}
func (s *crawlQueue) perform() {
	defer close(s.stopped)
	defer s.releaseAll()
	ctx, cancel := queueContext(s.done)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
		case <-s.wake:
		}
		if s.admission.enabled() {
			counts, e := s.Counts()
			if e != nil || counts["pending"] == 0 {
				continue
			}
		}
		release, e := s.begin(ctx)
		if e != nil {
			return
		}
		var next *types.Message
		err := s.dispatchUpdate(func(txn *badger.Txn) error {
			it := txn.NewIterator(badger.DefaultIteratorOptions)
			defer it.Close()
			prefix := []byte("pending\x00" + s.params.CrawlID + "\x00")
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				key := append([]byte(s.params.CrawlID+"\x00"), it.Item().Key()[len(prefix):]...)
				item, err := txn.Get(key)
				if err != nil {
					return err
				}
				var record crawlRecord
				if err := item.Value(func(data []byte) error { return json.Unmarshal(data, &record) }); err != nil {
					return err
				}
				if record.State != "pending" {
					return fmt.Errorf("invalid pending index")
				}
				record.State = "leased"
				data, _ := json.Marshal(record)
				if err := s.setRecord(txn, key, data); err != nil {
					return err
				}
				next = &record.Message
				return nil
			}
			return nil
		})
		if err == badger.ErrConflict {
			release()
			continue
		}
		if err != nil {
			release()
			log.Printf("crawl queue %s dispatch failed: %v", s.params.CrawlID, err)
			s.healthMu.Lock()
			s.lastError = err.Error()
			s.healthMu.Unlock()
			continue
		}
		s.healthMu.Lock()
		s.lastError = ""
		s.healthMu.Unlock()
		if next == nil {
			release()
		}
		if next != nil {
			s.hold(*next, release)
			if s.params.NotifyDispatch {
				select {
				case s.wake <- struct{}{}:
				default:
				}
			}
			select {
			case s.channel <- *next:
			case <-s.done:
				return
			}
		}
	}
}
func (s *crawlQueue) Stop() error {
	s.stopOnce.Do(func() { close(s.done) })
	s.startOnce.Do(func() { close(s.stopped) })
	<-s.stopped
	return nil
}
func (s *crawlQueue) Close() error {
	s.Stop()
	var err error
	s.closeOnce.Do(func() { s.dbMu.Lock(); defer s.dbMu.Unlock(); s.closed = true; err = s.db.Close() })
	return err
}
func (s *crawlQueue) GetChannel() chan types.Message { return s.channel }
func (s *crawlQueue) EndTransaction(msg types.Message, success bool) error {
	defer s.finish(msg)
	key, err := s.key(msg)
	if err != nil {
		return err
	}
	return s.update(context.Background(), func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		var record crawlRecord
		if err = item.Value(func(data []byte) error { return json.Unmarshal(data, &record) }); err != nil {
			return err
		}
		if record.Message.ID != msg.ID {
			return fmt.Errorf("stale crawl acknowledgment")
		}
		record.Message.LastError = msg.LastError
		record.Updated = time.Now().UnixNano()
		record.State = "completed"
		var incoming, existing message.FetcherRequest
		json.Unmarshal(msg.Message, &incoming)
		json.Unmarshal(record.Message.Message, &existing)
		if success && existing.BetterTraversalThan(incoming) {
			record.State = "pending"
		}
		if success {
			if err := txn.Delete(append([]byte("outputs\x00"), key...)); err != nil {
				return err
			}
		}
		if !success {
			record.Attempts++
			record.State = "pending"
			if record.Attempts >= s.params.MaxAttempts {
				record.State = "failed"
			}
		}
		data, _ := json.Marshal(record)
		return s.setRecord(txn, key, data)
	})
}
func (s *crawlQueue) Len() int64 {
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	if s.closed {
		return -1
	}
	var count int64
	err := s.db.View(func(txn *badger.Txn) error {
		if _, err := txn.Get([]byte("active_ready\x00" + s.params.CrawlID)); err == nil {
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			it := txn.NewIterator(opts)
			defer it.Close()
			prefix := []byte("active\x00" + s.params.CrawlID + "\x00")
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				count++
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
			if record.State == "pending" || record.State == "leased" {
				count++
			}
		}
		return nil
	})
	if err != nil {
		return -1
	}
	return count
}

func (s *crawlQueue) LoadOutputs(msg types.Message) ([]types.Message, bool, error) {
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	if s.closed {
		return nil, false, badger.ErrDBClosed
	}
	key, err := s.key(msg)
	if err != nil {
		return nil, false, err
	}
	var outputs []types.Message
	err = s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(append([]byte("outputs\x00"), key...))
		if err != nil {
			return err
		}
		return item.Value(func(data []byte) error { return json.Unmarshal(data, &outputs) })
	})
	if err == badger.ErrKeyNotFound {
		return nil, false, nil
	}
	return outputs, err == nil, err
}
func (s *crawlQueue) SaveOutputs(msg types.Message, outputs []types.Message) error {
	key, err := s.key(msg)
	if err != nil {
		return err
	}
	data, err := json.Marshal(outputs)
	if err != nil {
		return err
	}
	return s.update(context.Background(), func(txn *badger.Txn) error { return txn.Set(append([]byte("outputs\x00"), key...), data) })
}

func (s *crawlQueue) ReferencedObjects() ([]types.UUID, error) {
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	if s.closed {
		return nil, badger.ErrDBClosed
	}
	var result []types.UUID
	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			key := it.Item().Key()
			if bytes.HasPrefix(key, []byte("pending\x00")) || bytes.HasPrefix(key, []byte("active\x00")) || bytes.HasPrefix(key, []byte("active_ready\x00")) || bytes.HasPrefix(key, []byte("count\x00")) {
				continue
			}

			if err := it.Item().Value(func(data []byte) error {
				refs, err := objectReferences(data)
				if err != nil {
					return err
				}
				result = append(result, refs...)
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}

func (s *crawlQueue) pendingKey(key []byte) []byte { return append([]byte("pending\x00"), key...) }
func (s *crawlQueue) setRecord(txn *badger.Txn, key, data []byte) error {
	var record crawlRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return err
	}
	if _, err := txn.Get([]byte("active_ready\x00" + s.params.CrawlID)); err == nil {
		oldState := ""
		if item, e := txn.Get(key); e == nil {
			var old crawlRecord
			if err := item.Value(func(data []byte) error { return json.Unmarshal(data, &old) }); err != nil {
				return err
			}
			oldState = old.State
		} else if e != badger.ErrKeyNotFound {
			return e
		}
		if oldState != record.State {
			if oldState != "" {
				if err := s.adjustCount(txn, oldState, -1); err != nil {
					return err
				}
			}
			if err := s.adjustCount(txn, record.State, 1); err != nil {
				return err
			}
		}
	} else if err != badger.ErrKeyNotFound {
		return err
	}
	activeKey := append([]byte("active\x00"), key...)
	if record.State == "pending" || record.State == "leased" {
		if err := txn.Set(activeKey, nil); err != nil {
			return err
		}
	} else {
		if err := txn.Delete(activeKey); err != nil {
			return err
		}
	}
	var err error
	if record.State == "pending" {
		err = txn.Set(s.pendingKey(key), nil)
	} else {
		err = txn.Delete(s.pendingKey(key))
	}
	if err != nil {
		return err
	}
	return txn.Set(key, data)
}
func (s *crawlQueue) Health() string {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	return s.lastError
}

func (s *crawlQueue) update(ctx context.Context, fn func(*badger.Txn) error) error {
	defer func() {
		if s.params.NotifyDispatch {
			select {
			case s.wake <- struct{}{}:
			default:
			}
		}
	}()
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	if s.closed {
		return badger.ErrDBClosed
	}
	for attempt := 0; attempt < 16; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := s.transaction(fn)
		if err != badger.ErrConflict {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
	return badger.ErrConflict
}

func (s *crawlQueue) dispatchUpdate(fn func(*badger.Txn) error) error {
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	if s.closed {
		return badger.ErrDBClosed
	}
	return s.transaction(fn)
}

func (s *crawlQueue) adjustCount(tx *badger.Txn, state string, delta int64) error {
	key := []byte("count\x00" + s.params.CrawlID + "\x00" + state)
	var n int64
	item, err := tx.Get(key)
	if err == nil {
		if err := item.Value(func(b []byte) error { return json.Unmarshal(b, &n) }); err != nil {
			return err
		}
	} else if err != badger.ErrKeyNotFound {
		return err
	}
	n += delta
	data, err := json.Marshal(n)
	if err != nil {
		return err
	}
	return tx.Set(key, data)
}

// Queue mutations share state counters, so competing optimistic writers cannot
// commit concurrently. Serialize them to avoid conflict storms between the pump,
// producers and acknowledgments while retaining concurrent snapshot reads.
func (s *crawlQueue) transaction(fn func(*badger.Txn) error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.db.Update(fn)
}

func (s *crawlQueue) PauseDispatch(paused bool) { s.admission.pauseDispatch(paused) }
func (s *crawlQueue) DispatchActive() int       { return s.admission.dispatchActive() }
