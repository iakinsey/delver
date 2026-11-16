package util

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSharedOriginGateAndCancellation(t *testing.T) {
	g := NewOriginGate()
	var active, max int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := g.Acquire(context.Background(), "https://example.test", 2, 0)
			if err != nil {
				t.Error(err)
				return
			}
			n := atomic.AddInt32(&active, 1)
			for {
				old := atomic.LoadInt32(&max)
				if n <= old || atomic.CompareAndSwapInt32(&max, old, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&active, -1)
			release()
		}()
	}
	wg.Wait()
	if max != 2 {
		t.Fatal(max)
	}
	release, err := g.Acquire(context.Background(), "https://other.test", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Acquire(ctx, "https://other.test", 1, 0); err != context.Canceled {
		t.Fatal(err)
	}
}
func TestScopeBoundaries(t *testing.T) {
	if ScopeAllows("same_domain", "https://a.example.co.uk", "https://elsewhere.co.uk") {
		t.Fatal("merged public suffix domains")
	}
	if !ScopeAllows("same_domain", "https://a.example.co.uk", "https://b.example.co.uk") {
		t.Fatal("split related subdomains")
	}
	if ScopeAllows("same_origin", "https://example.test", "http://example.test") {
		t.Fatal("merged schemes")
	}
	if !ScopeAllows("same_origin", "https://example.test:443/", "https://example.test/x") {
		t.Fatal("split default port")
	}
	if ScopeAllows("same_domain", "http://127.0.0.1", "http://128.0.0.1") {
		t.Fatal("merged IP addresses")
	}
}

func TestOriginGateLimitChangesWhileBusy(t *testing.T) {
	g := NewOriginGate()
	first, err := g.Acquire(context.Background(), "dynamic", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := g.Acquire(context.Background(), "dynamic", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err = g.Acquire(ctx, "dynamic", 1, 0); err == nil {
		t.Fatal("lowered limit ignored")
	}
	first()
	second()
	release, err := g.Acquire(context.Background(), "dynamic", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	release()
	release()
}

func TestConfiguredOriginLimitAppliesToExistingWaiters(t *testing.T) {
	g := NewOriginGate()
	g.SetLimits(2, 0)
	first, err := g.Acquire(context.Background(), "host", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := g.Acquire(context.Background(), "host", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	admitted := make(chan func(), 1)
	go func() {
		release, err := g.Acquire(ctx, "host", 2, 0)
		if err == nil {
			admitted <- release
		}
	}()
	// Even a caller holding the old count must use the configured ceiling.
	g.SetLimits(1, 0)
	first()
	select {
	case release := <-admitted:
		release()
		t.Fatal("admitted above lowered host limit")
	case <-time.After(30 * time.Millisecond):
	}
	second()
	select {
	case release := <-admitted:
		release()
	case <-time.After(time.Second):
		t.Fatal("host waiter did not recover")
	}
	first, err = g.Acquire(ctx, "host", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	go func() {
		release, err := g.Acquire(ctx, "host", 1, 0)
		if err == nil {
			admitted <- release
		}
	}()
	g.SetLimits(2, 0)
	select {
	case release := <-admitted:
		release()
	case <-time.After(time.Second):
		t.Fatal("increased host limit did not wake existing waiter")
	}
}
