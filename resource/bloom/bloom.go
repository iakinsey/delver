package bloom

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/RoaringBitmap/roaring/roaring64"
	"github.com/pkg/errors"
	"github.com/twmb/murmur3"
)

type BloomFilter interface {
	SetString(string) error
	SetBytes([]byte) error
	SetMany([][]byte) error
	ContainsString(string) bool
	ContainsBytes([]byte) bool
	Save(string) (int64, error)
	Close()
}

type bloomError struct {
	msg      string
	maxN     uint64
	desiredP float64
	currentP float64
	kFloat   float64
}

func (e *bloomError) Error() string {
	return fmt.Sprintf("BloomError: %#v", e)
}

func IsBloomError(err error) bool {
	var target *bloomError
	return errors.As(err, &target)
}

func NewBloomError(b *bloomFilter, msg string) error {
	return &bloomError{
		msg:      msg,
		maxN:     b.maxN,
		desiredP: b.p,
		currentP: b.getCurrentP(),
		kFloat:   b.kFloat,
	}
}

type ErrBloomOverflow error
type ErrBloomExceedsErrorRate error

type bloomFilter struct {
	mutex   sync.RWMutex
	maxN    uint64
	p       float64
	pDigits int
	n       uint64
	mFloat  float64
	m       uint64
	kFloat  float64
	k       uint64
	bitmap  *roaring64.Bitmap
}

type savedBloom struct {
	MaxN   uint64
	P      float64
	N      uint64
	Bitmap []byte
}
type savedFilters struct {
	Version    int
	BloomCount int
	Filters    []savedBloom
}

func LoadBloomFilter(src io.Reader) (BloomFilter, error) {
	var saved savedFilters
	if err := json.NewDecoder(src).Decode(&saved); err != nil {
		return nil, err
	}
	if saved.Version != 1 || len(saved.Filters) == 0 {
		return nil, errors.New("invalid bloom format")
	}
	filters := make([]BloomFilter, 0, len(saved.Filters))
	for _, record := range saved.Filters {
		if record.MaxN == 0 || record.P <= 0 || record.P >= 1 {
			return nil, errors.New("invalid bloom parameters")
		}
		bitmap := roaring64.New()
		if err := bitmap.UnmarshalBinary(record.Bitmap); err != nil {
			return nil, err
		}
		b := newBloomFilter(record.MaxN, record.P, bitmap)
		b.n = record.N
		filters = append(filters, b)
	}
	if saved.BloomCount == 0 {
		return filters[0], nil
	}
	first := saved.Filters[0]
	return &rollingBloomFilter{blooms: filters, bloomCount: saved.BloomCount, maxN: first.MaxN, p: first.P, terminate: make(chan bool)}, nil
}
func saveFilters(path string, filters []BloomFilter, count int) (int64, error) {
	saved := savedFilters{Version: 1, BloomCount: count}
	for _, filter := range filters {
		b := filter.(*bloomFilter)
		b.mutex.RLock()
		data, err := b.bitmap.MarshalBinary()
		record := savedBloom{b.maxN, b.p, b.n, data}
		b.mutex.RUnlock()
		if err != nil {
			return 0, err
		}
		saved.Filters = append(saved.Filters, record)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		return 0, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".bloom-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	n, err := f.Write(data)
	if err != nil {
		return int64(n), err
	}
	if err = f.Sync(); err != nil {
		return int64(n), err
	}
	if err = f.Close(); err != nil {
		return int64(n), err
	}
	return int64(n), os.Rename(f.Name(), path)
}

type BloomFilterParams struct {
	MaxN uint64  `json:"max_n"`
	P    float64 `json:"p"`
}

func NewBloomFilter(params BloomFilterParams) BloomFilter {
	return newBloomFilter(params.MaxN, params.P, roaring64.New())
}

func newBloomFilter(maxN uint64, p float64, bitmap *roaring64.Bitmap) *bloomFilter {
	if maxN == 0 || p <= 0 || p >= 1 || math.IsNaN(p) {
		panic("bloom requires max_n > 0 and 0 < p < 1")
	}

	mFloat := getOptimalBloomM(maxN, p)
	m := uint64(math.Ceil(mFloat))
	if m < 2 {
		m = 2
	}
	kFloat := getOptimalBloomK(m, maxN, p)
	k := uint64(math.Ceil(kFloat))

	return &bloomFilter{
		maxN:    maxN,
		p:       p,
		n:       0,
		mFloat:  mFloat,
		m:       m,
		kFloat:  kFloat,
		k:       k,
		pDigits: 0,
		bitmap:  bitmap,
	}

}

func (s *bloomFilter) SetString(val string) error {
	return s.SetBytes([]byte(val))
}

func (s *bloomFilter) SetBytes(val []byte) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if err := s.checkBounds(); err != nil {
		return errors.Wrap(err, "failed bounds check when adding element")
	}

	s.bitmap.AddMany(s.getHashes(val))
	s.n += 1

	return nil
}

func (s *bloomFilter) SetMany(vals [][]byte) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if len(vals) == 0 {
		return nil
	}
	if uint64(len(vals)) > s.maxN-s.n {
		return NewBloomError(s, "batch exceeds capacity")
	}
	if err := s.checkBounds(); err != nil {
		return errors.Wrap(err, "failed bounds check when adding element")
	}

	var hashes []uint64

	for _, val := range vals {
		hashes = append(hashes, s.getHashes(val)...)
	}

	s.bitmap.AddMany(hashes)

	s.n += uint64(len(vals))

	return nil
}

func (s *bloomFilter) ContainsString(val string) bool {
	return s.ContainsBytes([]byte(val))
}

func (s *bloomFilter) ContainsBytes(val []byte) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	for _, hash := range s.getHashes(val) {
		if !s.bitmap.Contains(hash) {
			return false
		}
	}

	return true
}

func (s *bloomFilter) Save(path string) (int64, error) { return saveFilters(path, []BloomFilter{s}, 0) }

func (s *bloomFilter) Close() {}

func (s *bloomFilter) getHashes(in []byte) (result []uint64) {
	hasher := murmur3.New128()

	// murmur3 write function does not return errors
	hasher.Write(in)

	upper, lower := hasher.Sum128()

	for i := uint64(0); i < s.k; i++ {
		hash := (lower + (i * upper) + uint64(math.Pow(float64(i), 2))) % s.m
		result = append(result, hash)
	}

	return
}

func (s *bloomFilter) checkBounds() error {
	if s.n >= s.maxN {
		return ErrBloomOverflow(NewBloomError(s, "bloom filter size overflow"))
	} else if s.getCurrentP() >= s.p {
		return ErrBloomExceedsErrorRate(NewBloomError(s, "bloom filter exceeds error rate"))
	}

	return nil
}

func (s *bloomFilter) getCurrentP() float64 {
	e := math.E
	k := s.kFloat
	n := float64(s.n)
	m := s.mFloat
	p := math.Pow(1-math.Pow(e, -k*(n+0.5)/(m-1)), k)

	return p
}

func getOptimalBloomM(n uint64, p float64) float64 {
	return -(float64(n) * math.Log(p)) / math.Pow(math.Ln2, 2)
}

func getOptimalBloomK(m uint64, maxN uint64, p float64) float64 {
	return float64(m) / float64(maxN) * math.Ln2
}
