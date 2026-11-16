package maps

import (
	"encoding/json"
	"github.com/iakinsey/delver/config"
	"path/filepath"
	"testing"
	"time"
)

func TestRegressionCloseAfterGCExits(t *testing.T) {
	config.Set(json.RawMessage(`{"persistent_map":{"gc_interval":1000000,"gc_err_threshold":1}}`))
	defer config.Set(json.RawMessage(`{}`))
	m := NewPersistentMap(PersistentMapParams{Path: filepath.Join(t.TempDir(), "map")})
	time.Sleep(30 * time.Millisecond)
	done := make(chan struct{})
	go func() { m.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Close blocked after GC loop hit its error threshold")
	}
}
