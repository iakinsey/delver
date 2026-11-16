package maps

import (
	"encoding/base64"
	"fmt"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
	"time"
)

func TestConsumeHostRetainsFailedBatchAndSerializesWriters(t *testing.T) {
	root := t.TempDir()
	m := NewMultiHostMap(MultiHostMapParams{BasePath: root})
	defer m.Close()
	require.NoError(t, m.Set([]byte("http://example.test/old"), []byte("old")))
	hostPath := filepath.Join(root, base64.URLEncoding.EncodeToString([]byte("example.test")))
	require.Error(t, ConsumeHost(hostPath, func(Map) error { return fmt.Errorf("output unavailable") }))
	value, err := m.Get([]byte("http://example.test/old"))
	require.NoError(t, err)
	require.Equal(t, []byte("old"), value)
	entered, release := make(chan struct{}), make(chan struct{})
	consumed := make(chan error, 1)
	go func() { consumed <- ConsumeHost(hostPath, func(Map) error { close(entered); <-release; return nil }) }()
	<-entered
	writer := make(chan error, 1)
	go func() { writer <- m.Set([]byte("http://example.test/new"), []byte("new")) }()
	select {
	case <-writer:
		t.Fatal("writer entered an active domain drain")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-consumed)
	require.NoError(t, <-writer)
	value, err = m.Get([]byte("http://example.test/new"))
	require.NoError(t, err)
	require.Equal(t, []byte("new"), value)
}
