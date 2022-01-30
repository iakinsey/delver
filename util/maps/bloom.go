package maps

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strconv"

	"github.com/RoaringBitmap/roaring/roaring64"
	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
	"github.com/twmb/murmur3"
)

type bloomError struct {
	msg      string
	maxN     uint64
	desiredP float64
	currentP float64
	kFloat   float64
}

func (e *bloomError) Error() string {
	return fmt.Sprintf("%#v", e)
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

func LoadBloomFilter(src io.Reader) (Mapper, error) {
	scanner := bufio.NewScanner(src)

	if ok := scanner.Scan(); !ok {
		return nil, errors.New("failed to scan for bloom maxN value")
	}

	maxN, err := strconv.ParseUint(scanner.Text(), 10, 64)

	if err != nil {
		return nil, errors.Wrap(err, "failed to parse maxN value from bloom file")
	}

	if ok := scanner.Scan(); !ok {
		return nil, errors.New("failed to scan for bloom p value")
	}

	p, err := strconv.ParseFloat(scanner.Text(), 64)

	if err != nil {
		return nil, errors.Wrap(err, "failed to parse p value from bloom file")
	}

	bitmap := roaring64.New()

	if _, err := bitmap.ReadFrom(src); err != nil {
		return nil, errors.Wrap(err, "failed to parse bloom filter")
	}

	return newBloomFilter(maxN, p, bitmap), nil
}

func NewBloomFilter(maxN uint64, p float64) Mapper {
	return newBloomFilter(maxN, p, roaring64.New())
}

func newBloomFilter(maxN uint64, p float64, bitmap *roaring64.Bitmap) Mapper {
	mFloat := getOptimalBloomM(maxN, p)
	m := uint64(mFloat)
	kFloat := getOptimalBloomK(m, maxN, p)
	k := uint64(kFloat)

	return &bloomFilter{
		maxN:    maxN,
		p:       p,
		n:       0,
		mFloat:  mFloat,
		m:       m,
		kFloat:  kFloat,
		k:       k,
		pDigits: util.CountDecimals(p),
		bitmap:  bitmap,
	}

}

func (s *bloomFilter) AddString(val string) error {
	return s.AddBytes([]byte(val))
}

func (s *bloomFilter) AddBytes(val []byte) error {
	if err := s.checkBounds(); err != nil {
		return errors.Wrap(err, "failed bounds check when adding element")
	}

	s.bitmap.AddMany(s.getHashes(val))
	s.n += 1

	return nil
}

func (s *bloomFilter) ContainsString(val string) bool {
	return s.ContainsBytes([]byte(val))
}

func (s *bloomFilter) ContainsBytes(val []byte) bool {
	for _, hash := range s.getHashes(val) {
		if !s.bitmap.Contains(hash) {
			return false
		}
	}

	return true
}

func (s *bloomFilter) Size() uint64 {
	return s.bitmap.GetSizeInBytes()
}

func (s *bloomFilter) Save(dst io.Writer) (int64, error) {
	fmt.Fprintln(dst, strconv.FormatUint(s.maxN, 10))
	fmt.Fprintln(dst, strconv.FormatFloat(s.p, 'f', s.pDigits, 64))

	return s.bitmap.WriteTo(dst)
}

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
	} else if s.getCurrentP() > s.p {
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
	d := math.Pow(float64(10), float64(s.pDigits))

	return math.Ceil(p*d) / d
}

func getOptimalBloomM(n uint64, p float64) float64 {
	return -(float64(n) * math.Log(p)) * math.Pow(math.Ln2, 2)
}

func getOptimalBloomK(m uint64, maxN uint64, p float64) float64 {
	return float64(m/maxN) * math.Ln2
}
