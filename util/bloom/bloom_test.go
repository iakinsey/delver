package bloom

import (
	"os"
	"testing"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/util"
	"github.com/stretchr/testify/assert"
)

func TestBloomFilter(t *testing.T) {
	bloomFilter := NewBloomFilter(10000, 0.01)
	elementCount := 100
	var values []string

	for i := 0; i < elementCount; i++ {
		value := string(types.NewV4())
		values = append(values, value)
		assert.NoError(t, bloomFilter.SetString(value))
	}

	for _, value := range values {
		assert.True(t, bloomFilter.ContainsString(value))
	}

	for i := 0; i < elementCount/10; i++ {
		value := string(types.NewV4())
		assert.False(t, bloomFilter.ContainsString(value))
	}
}

func TestBloomFilterExceedsError(t *testing.T) {
	bloomFilter := NewBloomFilter(1, 0.01)

	assert.Error(t, bloomFilter.SetString(string(types.NewV4())))
}

func TestBloomPersist(t *testing.T) {
	bloomFilter := NewBloomFilter(10000, 0.01)
	elementCount := 50
	var values []string

	for i := 0; i < elementCount; i++ {
		value := string(types.NewV4())
		values = append(values, value)
		assert.NoError(t, bloomFilter.SetString(value))
	}

	bloomPath := util.NewTempPath("bloom")

	defer os.Remove(bloomPath)

	n, err := bloomFilter.Save(bloomPath)

	assert.NoError(t, err)
	assert.Greater(t, n, int64(0))

	f2, err := os.Open(bloomPath)

	assert.NoError(t, err)

	bloomFilter2, err := LoadBloomFilter(f2)

	assert.NoError(t, err)

	for _, value := range values {
		assert.True(t, bloomFilter2.ContainsString(value))
	}

	for i := 0; i < elementCount/10; i++ {
		value := string(types.NewV4())
		assert.False(t, bloomFilter2.ContainsString(value))
	}

}
