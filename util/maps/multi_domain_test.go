package maps

import (
	"fmt"
	"os"
	"testing"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/util"
	"github.com/stretchr/testify/assert"
)

func TestMultiMapHostGetAndSet(t *testing.T) {
	m := NewMultiHostMap(util.MakeTempFolder("multimapgetset"))
	count := 100
	var pairs [][2][]byte

	for i := 0; i < count; i++ {
		key := []byte(fmt.Sprintf("http://%s.com", util.RandomString(5)))
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

func TestMultiMapHostSetMany(t *testing.T) {
	path := util.MakeTempFolder("multimapgetset")

	defer os.RemoveAll(path)

	m := NewMultiHostMap(path)
	count := 10
	var pairs [][2][]byte

	for i := 0; i < count; i++ {
		host := util.RandomString(5)

		for j := 0; j < count; j++ {
			path := util.RandomString(5)
			key := []byte(fmt.Sprintf("http://%s.com/%s", host, path))
			val := []byte(types.NewV4())
			pairs = append(pairs, [2][]byte{key, val})
		}
	}

	assert.NoError(t, m.SetMany(pairs))

	for _, pair := range pairs {
		key, expectedVal := pair[0], pair[1]
		actualVal, err := m.Get(key)

		assert.NoError(t, err)
		assert.Equal(t, expectedVal, actualVal)
	}
}
