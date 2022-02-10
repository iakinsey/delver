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
	firstBloom := NewBloomFilter(maxN, p)
	v := []byte{1, 3, 5, 7, 9}

	assert.NoError(t, firstBloom.SetBytes(v))

	_, err := firstBloom.Save(path)

	assert.NoError(t, err)

	pBloom, err := NewPersistentRollingBloomFilter(3, maxN, p, path, conf.DefaultSaveInterval)

	assert.NotNil(t, pBloom)
	assert.NoError(t, err)
	assert.True(t, pBloom.ContainsBytes(v))
}

func TestCreateRollingBloomFileDoesntExist(t *testing.T) {
	path := util.NewTempPath("rolling-bloom-no-exist")

	defer os.RemoveAll(path)

	maxN := uint64(10000)
	p := 0.1

	pBloom, err := NewPersistentRollingBloomFilter(3, maxN, p, path, config.Get().DefaultSaveInterval)

	assert.NotNil(t, pBloom)
	assert.NoError(t, err)
}

func TestRollingBloomSetAndGet(t *testing.T) {
	bloom := NewRollingBloomFilter(3, 10000, 0.01)
	val := []byte{1, 2, 3, 4, 5}

	assert.NoError(t, bloom.SetBytes(val))

	assert.True(t, bloom.ContainsBytes(val))
}

func TestRollingBloomSetManyAndGet(t *testing.T) {
	bloom := NewRollingBloomFilter(3, 10000, 0.01)
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
	path := util.NewTempPath("rolling-bloom-exist")

	defer os.RemoveAll(path)

	assert.NoFileExists(t, path)

	maxN := uint64(10000)
	p := 0.1
	pBloom, err := NewPersistentRollingBloomFilter(3, maxN, p, path, config.Get().DefaultSaveInterval)

	assert.NotNil(t, pBloom)
	assert.NoError(t, err)
	pBloom.Close()
	assert.FileExists(t, path)
}
