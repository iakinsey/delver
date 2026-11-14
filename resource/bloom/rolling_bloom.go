package bloom

import (
	"os"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

type rollingBloomFilter struct {
	closeOnce    sync.Once
	done         chan struct{}
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
		panic(err)
	}

	return b
}

func newPersistentRollingBloomFilter(bloomCount int, maxN uint64, p float64, path string, saveInterval time.Duration) (BloomFilter, error) {
	rbf := newRollingBloomFilter(bloomCount, maxN, p).(*rollingBloomFilter)
	if f, err := os.Open(path); err == nil {
		loaded, loadErr := LoadBloomFilter(f)
		f.Close()
		if loadErr != nil {
			return nil, loadErr
		}
		switch b := loaded.(type) {
		case *rollingBloomFilter:
			rbf.blooms = b.blooms
			rbf.bloomCount = b.bloomCount
			rbf.maxN = b.maxN
			rbf.p = b.p
		case *bloomFilter:
			rbf.blooms = []BloomFilter{b}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	rbf.path = path
	rbf.saveInterval = saveInterval
	rbf.done = make(chan struct{})
	go rbf.handleSave()
	return rbf, nil
}

func newRollingBloomFilter(bloomCount int, maxN uint64, p float64) BloomFilter {
	if bloomCount < 1 {
		panic("bloom_count must be positive")
	}
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
	for _, v := range vals {
		if err := s.SetBytes(v); err != nil {
			return err
		}
	}
	return nil
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
	return saveFilters(path, s.blooms, s.bloomCount)
}

func (s *rollingBloomFilter) Close() {
	s.closeOnce.Do(func() {
		close(s.terminate)
		if s.done != nil {
			<-s.done
		}
		if s.path != "" {
			if _, err := s.Save(s.path); err != nil {
				log.Error(err)
			}
		}
	})
}

func (s *rollingBloomFilter) rotate() { s.rwLock.Lock(); defer s.rwLock.Unlock(); s.rotateLocked() }
func (s *rollingBloomFilter) rotateLocked() {

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

	s.rotateLocked()

	result := fn(s.blooms[0])

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
	defer close(s.done)
	if s.path == "" || s.saveInterval <= 0 {
		return
	}

	for {
		select {
		case <-time.After(s.saveInterval):
			if _, err := s.Save(s.path); err != nil {
				log.Error(err)
			}
		case <-s.terminate:
			return
		}
	}
}
