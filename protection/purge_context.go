package protection

import (
	"context"
	"fmt"
)

// Disabling a source cancels its outstanding destructive operation. Independent
// restricted quarantines and other lists retain their own enforcement.
func (p *Policy) PurgeContext(parent context.Context, id string) (context.Context, func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	active := false
	for _, i := range p.state.Incidents {
		if i.ID == id {
			active = i.State == "pending" || i.State == "running"
			break
		}
	}
	if !active {
		return nil, nil, ErrCancelled
	}
	if p.purgeCancels[id] != nil {
		return nil, nil, fmt.Errorf("cleanup already running")
	}
	ctx, cancel := context.WithCancel(parent)
	p.purgeCancels[id] = cancel
	return ctx, func() { cancel(); p.mu.Lock(); delete(p.purgeCancels, id); p.mu.Unlock() }, nil
}
