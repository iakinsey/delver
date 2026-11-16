package bloom

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRegressionWrappedOverflow(t *testing.T) {
	b := NewBloomFilter(BloomFilterParams{MaxN: 10, P: 0.01}).(*bloomFilter)
	b.n = b.maxN
	err := b.SetString("next")
	if err == nil || !IsBloomError(err) {
		t.Fatalf("overflow not recognized: %v", err)
	}
}
func TestRegressionRollingSaturation(t *testing.T) {
	b := NewRollingBloomFilter(RollingBloomFilterParams{BloomCount: 3, MaxN: 10, P: 0.01})
	for i := 0; i < 12; i++ {
		if err := b.SetBytes([]byte{byte(i)}); err != nil {
			t.Fatalf("insert %d should rotate: %v", i, err)
		}
	}
}
func TestRegressionRollingCloseMemory(t *testing.T) {
	b := NewRollingBloomFilter(RollingBloomFilterParams{BloomCount: 3, MaxN: 10, P: 0.01})
	done := make(chan struct{})
	go func() { b.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Close blocked without saver goroutine")
	}
}
func TestRegressionRollingRetainsGenerations(t *testing.T) {
	b := newRollingBloomFilter(3, 1000, 0.000001).(*rollingBloomFilter)
	b.SetString("older-generation")
	b.rotate()
	b.SetString("newest-generation")
	path := filepath.Join(t.TempDir(), "bloom")
	b.Save(path)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	loaded, err := LoadBloomFilter(f)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.ContainsString("older-generation") {
		t.Fatal("saved filter loses retained older generation")
	}
}
func TestRegressionBloomReloadCapacity(t *testing.T) {
	b := NewBloomFilter(BloomFilterParams{MaxN: 100, P: 0.01}).(*bloomFilter)
	b.SetString("one")
	path := filepath.Join(t.TempDir(), "bloom")
	b.Save(path)
	f, _ := os.Open(path)
	defer f.Close()
	loaded, err := LoadBloomFilter(f)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.(*bloomFilter).n != b.n {
		t.Fatalf("insert count before=%d after=%d", b.n, loaded.(*bloomFilter).n)
	}
}
