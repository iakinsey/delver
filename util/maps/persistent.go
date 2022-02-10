package maps

import (
	"time"

	"github.com/iakinsey/delver/config"
	log "github.com/sirupsen/logrus"

	badger "github.com/dgraph-io/badger/v3"
	"github.com/pkg/errors"
)

type persistentMap struct {
	db        *badger.DB
	terminate chan bool
	conf      config.PersistentMapConfig
}

func NewPersistentMap(path string, conf config.PersistentMapConfig) Map {
	opts := badger.DefaultOptions(path)
	opts.Logger = nil
	db, err := badger.Open(opts)

	if err != nil {
		log.Fatal(err)
	}

	m := &persistentMap{
		db:        db,
		terminate: make(chan bool),
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
			result = val

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
	if err := s.db.Close(); err != nil {
		log.Errorf("error when closing persistentMap: %s", err)
	}
	s.terminate <- true
}

func (s *persistentMap) handleGc() {
	errs := 0

	for {
		select {
		case <-time.After(s.conf.GCInterval):
			if err := s.db.RunValueLogGC(s.conf.GCDiscardRatio); err != nil {
				log.Error(errors.Wrap(err, "persistentMap gc error"))
				errs += 1
			}
		case <-s.terminate:
			return
		}

		if errs == s.conf.GCErrThreshold {
			return
		}
	}
}
