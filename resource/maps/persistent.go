package maps

import (
	"sync"
	"time"

	"github.com/iakinsey/delver/config"
	log "github.com/sirupsen/logrus"

	badger "github.com/dgraph-io/badger/v3"
	"github.com/pkg/errors"
)

type persistentMap struct {
	done      chan struct{}
	closeOnce sync.Once
	db        *badger.DB
	terminate chan bool
	conf      config.PersistentMapConfig
}

type PersistentMapParams struct {
	Path string `json:"path"`
}

func NewPersistentMap(params PersistentMapParams) Map {
	return NewPersistentMapWithConfig(params, config.Get().PersistentMap)
}
func NewPersistentMapWithConfig(params PersistentMapParams, c config.PersistentMapConfig) Map {
	opts := badger.DefaultOptions(params.Path)
	opts.Logger = nil
	opts.SyncWrites = true
	db, err := badger.Open(opts)

	if err != nil {
		panic(err)
	}

	m := &persistentMap{
		db:        db,
		done:      make(chan struct{}),
		terminate: make(chan bool),
		conf:      c,
	}

	go m.handleGc()

	return m
}

func (s *persistentMap) Get(key []byte) ([]byte, error) {
	var result []byte

	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)

		if err == badger.ErrKeyNotFound {
			return ErrKeyNotFound
		} else if err != nil {
			return errors.Wrap(err, "failed to get key")
		}

		err = item.Value(func(val []byte) error {
			result = append([]byte(nil), val...)

			return nil
		})

		if err != nil {
			return errors.Wrap(err, "failed to get value")
		}

		return nil
	})

	if err == ErrKeyNotFound {
		return nil, err
	} else if err != nil {
		return nil, errors.Wrap(err, "failed Get transaction")
	}

	return result, nil
}

func (s *persistentMap) Set(key []byte, val []byte) error {
	err := s.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, val)
	})

	if err != nil {
		return errors.Wrap(err, "failed Set transaction")
	}

	return nil
}

func (s *persistentMap) SetMany(pairs [][2][]byte) error {
	err := s.db.Update(func(txn *badger.Txn) error {
		for _, pair := range pairs {
			key, val := pair[0], pair[1]

			err := txn.Set(key, val)

			if err != nil {
				return errors.Wrap(err, "failed set operation")
			}
		}

		return nil
	})

	if err != nil {
		return errors.Wrap(err, "failed SetMany transaction")
	}

	return nil
}

func (s *persistentMap) Iter(fn func([]byte, []byte) error) error {
	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = s.conf.DefaultPrefetchSize

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			k := item.Key()
			err := item.Value(func(v []byte) error {
				return fn(k, v)
			})

			if err != nil {
				return errors.Wrap(err, "failed to get value")
			}
		}
		return nil
	})

	if err != nil {
		return errors.Wrap(err, "failed IterKeys transaction")
	}

	return nil
}

func (s *persistentMap) Close() {
	s.closeOnce.Do(func() {
		close(s.terminate)
		<-s.done
		if err := s.db.Close(); err != nil {
			log.Error(err)
		}
	})
}
func (s *persistentMap) handleGc() {
	defer close(s.done)
	if s.conf.GCInterval <= 0 {
		return
	}
	failures := 0
	timer := time.NewTicker(s.conf.GCInterval)
	defer timer.Stop()
	for {
		select {
		case <-s.terminate:
			return
		case <-timer.C:
			if err := s.db.RunValueLogGC(s.conf.GCDiscardRatio); err != nil && err != badger.ErrNoRewrite {
				log.Error(err)
				failures++
				if s.conf.GCErrThreshold > 0 && failures >= s.conf.GCErrThreshold {
					return
				}
			} else {
				failures = 0
			}
		}
	}
}
