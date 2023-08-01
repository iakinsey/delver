package bloom

import (
	"os"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pkg/errors"
)

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

type RollingBloomFilterParams struct {
	BloomCount   int           `json:"bloom_count"`
	MaxN         uint64        `json:"max_n"`
	P            float64       `json:"p"`
	Path         string        `json:"path"`
	SaveInterval time.Duration `json:"save_interval"`
}

func NewRollingBloomFilter(params RollingBloomFilterParams) BloomFilter {
	if params.Path == "" {
		return newRollingBloomFilter(params.BloomCount, params.MaxN, params.P)
	}

	b, err := newPersistentRollingBloomFilter(
		params.BloomCount,
		params.MaxN,
		params.P,
		params.Path,
		params.SaveInterval,
	)

	if err != nil {
		log.Fatalf("failed to create persistent rolling bloom filter: %s", err)
	}

	return b
}

func newPersistentRollingBloomFilter(bloomCount int, maxN uint64, p float64, path string, saveInterval time.Duration) (BloomFilter, error) {
	rbf := &rollingBloomFilter{
		rwLock:       sync.RWMutex{},
		saveInterval: saveInterval,
		terminate:    make(chan bool),
		bloomCount:   bloomCount,
		maxN:         maxN,
		p:            p,
		path:         path,
	}

	bloomParams := BloomFilterParams{
		MaxN: maxN,
		P:    p,
	}

	if path == "" {
		rbf.blooms = []BloomFilter{NewBloomFilter(bloomParams)}
		return rbf, nil
	} else if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		rbf.blooms = []BloomFilter{NewBloomFilter(bloomParams)}
	} else if err != nil {
		return nil, errors.Wrap(err, "failed to check if bloom file exists")
	} else if f, err := os.Open(path); err != nil {
		return nil, errors.Wrap(err, "failed to open existing bloom file")
	} else if bloom, err := LoadBloomFilter(f); err != nil {
		// Load a new bloom filter if the exiting one doesn't work
		rbf.blooms = []BloomFilter{NewBloomFilter(bloomParams)}
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

func newRollingBloomFilter(bloomCount int, maxN uint64, p float64) BloomFilter {
	bloomParams := BloomFilterParams{
		MaxN: maxN,
		P:    p,
	}
	rbf := &rollingBloomFilter{
		blooms:     []BloomFilter{NewBloomFilter(bloomParams)},
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
			log.Errorln("failed to save bloom filter while closing")
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

	params := BloomFilterParams{
		MaxN: s.maxN,
		P:    s.p,
	}

	if len(s.blooms) == s.bloomCount {
		s.blooms = append(
			[]BloomFilter{NewBloomFilter(params)},
			s.blooms[:len(s.blooms)-1]...,
		)
	} else {
		s.blooms = append(
			[]BloomFilter{NewBloomFilter(params)},
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
	if s.path == "" || s.saveInterval <= 0 {
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
