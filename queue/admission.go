package queue

import (
	"context"
	"github.com/iakinsey/delver/types"
	"sync"
)

// Admission is optional queue execution admission. A token precedes a durable
// lease and remains owned by the delivery until its transaction ends.
type admission struct {
	mu       sync.Mutex
	gate     func(context.Context) (func(), error)
	held     map[string]func()
	paused   bool
	changed  chan struct{}
	inflight int
}

func (a *admission) SetDispatchGate(g func(context.Context) (func(), error)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.gate = g
	if a.held == nil {
		a.held = map[string]func(){}
	}
}

// PausableDispatch stops new durable leases without consuming the saved backlog.
// DispatchActive includes deliveries offered to workers but not yet acknowledged.
type PausableDispatch interface {
	PauseDispatch(bool)
	DispatchActive() int
}

func (a *admission) pauseDispatch(paused bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.paused = paused
	if a.changed != nil {
		close(a.changed)
	}
	a.changed = make(chan struct{})
}
func (a *admission) dispatchActive() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.inflight
}
func (a *admission) begin(ctx context.Context) (func(), error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		a.mu.Lock()
		if a.paused {
			if a.changed == nil {
				a.changed = make(chan struct{})
			}
			changed := a.changed
			a.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-changed:
				continue
			}
		}
		g := a.gate
		a.mu.Unlock()
		release := func() {}
		if g != nil {
			var err error
			release, err = g(ctx)
			if err != nil {
				return nil, err
			}
		}
		a.mu.Lock()
		if a.paused {
			a.mu.Unlock()
			release()
			continue
		}
		a.inflight++
		a.mu.Unlock()
		var once sync.Once
		return func() {
			once.Do(func() {
				release()
				a.mu.Lock()
				a.inflight--
				a.mu.Unlock()
			})
		}, nil
	}
}
func deliveryKey(m types.Message) string {
	if m.DeliveryID != "" {
		return m.DeliveryID
	}
	return m.ID
}
func (a *admission) hold(m types.Message, release func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.held == nil {
		a.held = map[string]func(){}
	}
	a.held[deliveryKey(m)] = release
}
func (a *admission) finish(m types.Message) {
	a.mu.Lock()
	release := a.held[deliveryKey(m)]
	delete(a.held, deliveryKey(m))
	a.mu.Unlock()
	if release != nil {
		release()
	}
}
func (a *admission) releaseAll() {
	a.mu.Lock()
	held := a.held
	a.held = map[string]func(){}
	a.mu.Unlock()
	for _, release := range held {
		release()
	}
}
func queueContext(done <-chan struct{}) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-done:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func (a *admission) enabled() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.gate != nil }
