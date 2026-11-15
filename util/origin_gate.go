package util

import (
	"container/list"
	"context"
	"sync"
	"time"
)

type gateEntry struct {
	key   string
	slots chan struct{}
	mu    sync.Mutex
	next  time.Time
	refs  int
	idle  *list.Element
}

// OriginGate is shared by all HTTP clients belonging to one runtime or workspace.
// Idle entries are evicted only after their reserved cooldown has elapsed.
type OriginGate struct {
	mu      sync.Mutex
	entries map[string]*gateEntry
	idle    list.List
}

func NewOriginGate() *OriginGate { return &OriginGate{entries: map[string]*gateEntry{}} }
func (g *OriginGate) Acquire(ctx context.Context, key string, count int, delay time.Duration) (func(), error) {
	if count < 1 {
		count = 1
	}
	g.mu.Lock()
	for len(g.entries) >= 8192 && g.idle.Len() > 0 {
		front := g.idle.Front()
		e := front.Value.(*gateEntry)
		e.mu.Lock()
		ready := !time.Now().Before(e.next)
		e.mu.Unlock()
		if !ready {
			break
		}
		g.idle.Remove(front)
		delete(g.entries, e.key)
	}
	e := g.entries[key]
	if e == nil {
		e = &gateEntry{key: key, slots: make(chan struct{}, count)}
		g.entries[key] = e
	}
	if e.idle != nil {
		g.idle.Remove(e.idle)
		e.idle = nil
	}
	e.refs++
	g.mu.Unlock()
	forget := func() {
		g.mu.Lock()
		e.refs--
		if e.refs == 0 {
			e.idle = g.idle.PushBack(e)
		}
		g.mu.Unlock()
	}
	select {
	case e.slots <- struct{}{}:
	case <-ctx.Done():
		forget()
		return nil, ctx.Err()
	}
	var once sync.Once
	release := func() { once.Do(func() { <-e.slots; forget() }) }
	e.mu.Lock()
	wait := time.Until(e.next)
	if wait < 0 {
		wait = 0
	}
	e.next = time.Now().Add(wait + delay)
	e.mu.Unlock()
	if err := waitHTTP(ctx, wait); err != nil {
		release()
		return nil, err
	}
	return release, nil
}
