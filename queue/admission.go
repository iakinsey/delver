package queue

import (
	"context"
	"github.com/iakinsey/delver/types"
	"sync"
)

// Admission is optional queue execution admission. A token precedes a durable
// lease and remains owned by the delivery until its transaction ends.
type admission struct {
	mu   sync.Mutex
	gate func(context.Context) (func(), error)
	held map[string]func()
}

func (a *admission) SetDispatchGate(g func(context.Context) (func(), error)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.gate = g
	if a.held == nil {
		a.held = map[string]func(){}
	}
}
func (a *admission) begin(ctx context.Context) (func(), error) {
	a.mu.Lock()
	g := a.gate
	a.mu.Unlock()
	if g == nil {
		return func() {}, nil
	}
	return g(ctx)
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
