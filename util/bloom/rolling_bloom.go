package bloom

import (
	"log"
	"os"
	"sync"
	"time"

	"github.com/pkg/errors"
)

const defaultSaveInterval = 10 * time.Minute

type rollingBloomFilter struct {
	blooms       []BloomFilter
	rwLock       sync.RWMutex
	saveInterval time.Duration
	terminate    chan bool
	bloomCount   int
	maxN         uint64
	p            float64
	path         string
}

func NewPersistentRollingBloomFilter(bloomCount int, maxN uint64, p float64, path string) (BloomFilter, error) {
	rbf := &rollingBloomFilter{
		rwLock:       sync.RWMutex{},
		saveInterval: defaultSaveInterval,
		terminate:    make(chan bool),
		bloomCount:   bloomCount,
		maxN:         maxN,
		p:            p,
		path:         path,
	}

	if path == "" {
		rbf.blooms = []BloomFilter{NewBloomFilter(maxN, p)}
		return rbf, nil
	} else if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		rbf.blooms = []BloomFilter{NewBloomFilter(maxN, p)}
	} else if err != nil {
		return nil, errors.Wrap(err, "failed to check if bloom file exists")
	} else if f, err := os.Open(path); err != nil {
		return nil, errors.Wrap(err, "failed to open existing bloom file")
	} else if bloom, err := LoadBloomFilter(f); err != nil {
		return nil, errors.Wrap(err, "failed to load bloom filter")
	} else {
		defer f.Close()
		if bloomStruct, ok := bloom.(*bloomFilter); !ok {
			log.Fatalf("failed to cast bloom filter to struct form")
		} else {
			rbf.blooms = []BloomFilter{bloom}
			rbf.maxN = bloomStruct.maxN
			rbf.p = bloomStruct.p
		}
	}

	go rbf.handleSave()

	return rbf, nil
}

func NewRollingBloomFilter(bloomCount int, maxN uint64, p float64) BloomFilter {
	rbf := &rollingBloomFilter{
		blooms:     []BloomFilter{NewBloomFilter(maxN, p)},
		rwLock:     sync.RWMutex{},
		terminate:  make(chan bool),
		bloomCount: bloomCount,
		maxN:       maxN,
		p:          p,
	}

	return rbf
}

func (s *rollingBloomFilter) SetString(val string) error {
	return s.SetBytes([]byte(val))
}

func (s *rollingBloomFilter) SetBytes(val []byte) error {
	return s.writeTransaction(func(bf BloomFilter) error {
		return bf.SetBytes(val)
	})
}

func (s *rollingBloomFilter) SetMany(vals [][]byte) error {
	return s.writeTransaction(func(bf BloomFilter) error {
		return bf.SetMany(vals)
	})
}

func (s *rollingBloomFilter) ContainsString(val string) bool {
	return s.ContainsBytes([]byte(val))
}

func (s *rollingBloomFilter) ContainsBytes(val []byte) bool {
	return s.readTransaction(func(bf BloomFilter) bool {
		return bf.ContainsBytes(val)
	})
}

func (s *rollingBloomFilter) Save(path string) (int64, error) {
	s.rwLock.Lock()
	defer s.rwLock.Unlock()
	bf := s.blooms[0]

	return bf.Save(path)
}

func (s *rollingBloomFilter) Close() {
	if s.path != "" {
		if _, err := s.Save(s.path); err != nil {
			log.Printf("failed to save bloom filter while closing")
		}
	}

	for _, bloom := range s.blooms {
		bloom.Close()
	}

	s.terminate <- true
}

func (s *rollingBloomFilter) rotate() {
	s.rwLock.Lock()
	defer s.rwLock.Unlock()

	if len(s.blooms) == s.bloomCount {
		s.blooms = append(
			[]BloomFilter{NewBloomFilter(s.maxN, s.p)},
			s.blooms[:len(s.blooms)-1]...,
		)
	} else {
		s.blooms = append(
			[]BloomFilter{NewBloomFilter(s.maxN, s.p)},
			s.blooms...,
		)
	}
}

func (s *rollingBloomFilter) writeTransaction(fn func(BloomFilter) error) error {
	s.rwLock.Lock()
	defer s.rwLock.Unlock()

	currentBloom := s.blooms[0]

	if err := fn(currentBloom); err == nil {
		return nil
	} else if !IsBloomError(err) {
		return err
	}

	s.rotate()

	result := fn(currentBloom)

	return result
}

func (s *rollingBloomFilter) readTransaction(fn func(BloomFilter) bool) bool {
	s.rwLock.RLock()
	defer s.rwLock.RUnlock()

	for _, bloom := range s.blooms {
		if fn(bloom) {
			return true
		}
	}

	return false
}

func (s *rollingBloomFilter) handleSave() {
	if s.path == "" {
		return
	}

	for {
		select {
		case <-time.After(s.saveInterval):
			s.rwLock.Lock()
			s.blooms[0].Save(s.path)
			s.rwLock.Unlock()
		case <-s.terminate:
			return
		}
	}
}
