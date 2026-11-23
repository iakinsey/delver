package maps

import (
	"github.com/iakinsey/delver/config"
	"path/filepath"
	"testing"
	"time"
)

func TestRegressionCloseAfterGCExits(t *testing.T) {
	c := config.Get().PersistentMap
	c.GCInterval = time.Millisecond
	c.GCErrThreshold = 1
	// Invalid GC input produces a real error, unlike ErrNoRewrite. Wait for
	// the actual exit instead of assuming a 30 ms sleep reached the threshold.
	c.GCDiscardRatio = 0
	m := NewPersistentMapWithConfig(PersistentMapParams{Path: filepath.Join(t.TempDir(), "map")}, c).(*persistentMap)
	select {
	case <-m.done:
	case <-time.After(5 * time.Second):
		m.Close()
		t.Fatal("GC loop did not exit at its error threshold")
	}
	done := make(chan struct{})
	go func() { m.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close blocked after GC loop exited")
	}
}
