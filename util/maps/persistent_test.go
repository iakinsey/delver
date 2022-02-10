package maps

import (
	"testing"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/util"
	"github.com/stretchr/testify/assert"
)

func TestPersistentMapGetAndSet(t *testing.T) {
	m := NewPersistentMap(util.MakeTempFolder("mapgetset"), config.Get().PersistentMap)
	count := 100
	var pairs [][2][]byte

	for i := 0; i < count; i++ {
		key := []byte(types.NewV4())
		val := []byte(types.NewV4())
		err := m.Set(key, val)
		pairs = append(pairs, [2][]byte{key, val})

		assert.NoError(t, err)
	}

	for _, pair := range pairs {
		key, expectedVal := pair[0], pair[1]
		actualVal, err := m.Get(key)

		assert.NoError(t, err)
		assert.Equal(t, expectedVal, actualVal)
	}
}
func TestPersistentMapGetNoValue(t *testing.T) {
	m := NewPersistentMap(util.MakeTempFolder("getnovalue"), config.Get().PersistentMap)
	val, err := m.Get([]byte(types.NewV4()))

	assert.Nil(t, val)
	assert.EqualError(t, err, ErrKeyNotFound.Error())
}
func TestPersistentMapSetManyAndIter(t *testing.T) {
	m := NewPersistentMap(util.MakeTempFolder("setmanyiter"), config.Get().PersistentMap)
	count := 100
	var keys [][]byte
	var vals [][]byte
	var pairs [][2][]byte

	for i := 0; i < count; i++ {
		key := []byte(types.NewV4())
		val := []byte(types.NewV4())
		keys = append(keys, key)
		vals = append(vals, val)
		pairs = append(pairs, [2][]byte{key, val})
	}

	assert.NoError(t, m.SetMany(pairs))
	assert.NoError(t, m.Iter(func(k, v []byte) error {
		assert.True(t, util.ByteArrayInSlice(k, keys))
		assert.True(t, util.ByteArrayInSlice(v, vals))

		return nil
	}))
}
