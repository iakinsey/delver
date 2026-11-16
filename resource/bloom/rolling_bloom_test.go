package bloom

import (
	"os"
	"testing"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/util"
	"github.com/stretchr/testify/assert"
)

func TestCreateRollingBloomFileExists(t *testing.T) {
	conf := config.Get()
	path := util.NewTempPath("rolling-bloom-exist")

	defer os.RemoveAll(path)

	maxN := uint64(10000)
	p := 0.1
	bloomParams := BloomFilterParams{MaxN: maxN, P: p}
	firstBloom := NewBloomFilter(bloomParams)
	v := []byte{1, 3, 5, 7, 9}

	assert.NoError(t, firstBloom.SetBytes(v))

	_, err := firstBloom.Save(path)

	assert.NoError(t, err)

	rollingParams := RollingBloomFilterParams{
		BloomCount:   3,
		MaxN:         maxN,
		P:            p,
		Path:         path,
		SaveInterval: conf.DefaultSaveInterval,
	}
	pBloom := NewRollingBloomFilter(rollingParams)

	assert.NotNil(t, pBloom)
	assert.True(t, pBloom.ContainsBytes(v))
}

func TestCreateRollingBloomFileDoesntExist(t *testing.T) {
	path := util.NewTempPath("rolling-bloom-no-exist")
	conf := config.Get()

	defer os.RemoveAll(path)

	maxN := uint64(10000)
	p := 0.1
	rollingParams := RollingBloomFilterParams{
		BloomCount:   3,
		MaxN:         maxN,
		P:            p,
		Path:         path,
		SaveInterval: conf.DefaultSaveInterval,
	}

	pBloom := NewRollingBloomFilter(rollingParams)

	assert.NotNil(t, pBloom)
}

func TestRollingBloomSetAndGet(t *testing.T) {
	rollingParams := RollingBloomFilterParams{
		BloomCount: 3,
		MaxN:       10000,
		P:          0.01,
	}
	bloom := NewRollingBloomFilter(rollingParams)
	val := []byte{1, 2, 3, 4, 5}

	assert.NoError(t, bloom.SetBytes(val))

	assert.True(t, bloom.ContainsBytes(val))
}

func TestRollingBloomSetManyAndGet(t *testing.T) {
	rollingParams := RollingBloomFilterParams{
		BloomCount: 3,
		MaxN:       10000,
		P:          0.01,
	}
	bloom := NewRollingBloomFilter(rollingParams)
	vals := [][]byte{
		{1, 2, 3, 4, 5},
		{6, 7, 8, 9, 10},
		{11, 12, 13, 14, 15},
	}

	assert.NoError(t, bloom.SetMany(vals))

	for _, val := range vals {
		assert.True(t, bloom.ContainsBytes(val))
	}
}

func TestRollingBloomClose(t *testing.T) {
	conf := config.Get()
	path := util.NewTempPath("rolling-bloom-exist")

	defer os.RemoveAll(path)

	assert.NoFileExists(t, path)

	rollingParams := RollingBloomFilterParams{
		BloomCount:   3,
		MaxN:         10000,
		P:            0.1,
		Path:         path,
		SaveInterval: conf.DefaultSaveInterval,
	}
	pBloom := NewRollingBloomFilter(rollingParams)

	assert.NotNil(t, pBloom)
	pBloom.Close()
	assert.FileExists(t, path)
}
