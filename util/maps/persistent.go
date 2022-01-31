package maps

import (
	"log"
	"time"

	badger "github.com/dgraph-io/badger/v3"
	"github.com/pkg/errors"
)

const gcInterval = 5 * time.Minute
const gcDiscardRatio = 0.7
const gcErrThreshold = 2

type persistentMap struct {
	db        *badger.DB
	terminate chan bool
}

func NewPersistentMap(path string) Map {
	db, err := badger.Open(badger.DefaultOptions(path))

	if err != nil {
		log.Fatal(err)
	}

	m := &persistentMap{
		db: db,
	}

	go m.handleGc()

	return m
}

func (s *persistentMap) Get(key []byte) ([]byte, error) {
	var result []byte

	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)

		if err != nil {
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

	if err != nil {
		return nil, errors.Wrap(err, "failed get transaction")
	}

	return result, nil
}

func (s *persistentMap) Set(key []byte, val []byte) error {
	err := s.db.Update(func(txn *badger.Txn) error {
		err := txn.Set(key, val)

		if err != nil {
			return errors.Wrap(err, "failed set operation")
		}

		if err := txn.Commit(); err != nil {
			return errors.Wrap(err, "failed to commit set operaiton")
		}

		return nil
	})

	if err != nil {
		return errors.Wrap(err, "failed set transaction")
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

		if err := txn.Commit(); err != nil {
			return errors.Wrap(err, "failed to commit set operaiton")
		}

		return nil
	})

	if err != nil {
		return errors.Wrap(err, "failed set transaction")
	}

	return nil
}

func (s *persistentMap) IterKeys(func([]byte) error) error {
	return nil
}

func (s *persistentMap) Close() {
	s.terminate <- true
}

func (s *persistentMap) handleGc() {
	errs := 0

	for {
		select {
		case <-time.After(gcInterval):
			if err := s.db.RunValueLogGC(gcDiscardRatio); err != nil {
				log.Println(errors.Wrap(err, "persistentMap gc error").Error())
				errs += 1
			}
		case <-s.terminate:
			return
		}

		if errs == gcErrThreshold {
			return
		}
	}
}
