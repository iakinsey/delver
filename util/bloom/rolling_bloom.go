package bloom

import (
	"io"
	"log"
	"sync"
)

type rollingBloomFilter struct {
	blooms     []BloomFilter
	rwLock     sync.RWMutex
	bloomCount int
	maxN       uint64
	p          float64
}

func LoadRollingBloomFilter(bloomCount int, src io.Reader) (BloomFilter, error) {
	bloom, err := LoadBloomFilter(src)

	if err != nil {
		return nil, err
	}

	bloomStruct, ok := bloom.(*bloomFilter)

	if !ok {
		log.Fatalf("failed to cast bloom filter to struct form")
	}

	return &rollingBloomFilter{
		blooms:     []BloomFilter{bloom},
		rwLock:     sync.RWMutex{},
		bloomCount: bloomCount,
		maxN:       bloomStruct.maxN,
		p:          bloomStruct.p,
	}, nil
}

func NewRollingBloomFilter(bloomCount int, maxN uint64, p float64) BloomFilter {
	return &rollingBloomFilter{
		blooms:     []BloomFilter{NewBloomFilter(maxN, p)},
		rwLock:     sync.RWMutex{},
		bloomCount: bloomCount,
		maxN:       maxN,
		p:          p,
	}
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
	defer s.rwLock.Unlock()

	for _, bloom := range s.blooms {
		if fn(bloom) {
			return true
		}
	}

	return false
}
