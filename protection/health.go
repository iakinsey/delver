package protection

import "context"

// HealthError is lock-free so queue and frontier checks can call it while
// holding their own storage locks.
func (p *Policy) HealthError() error {
	if p == nil {
		return nil
	}
	index := p.current.Load()
	if index == nil || index.unavailable {
		return ErrUnavailable
	}
	return nil
}
func (p *Policy) Health() string {
	if e := p.HealthError(); e != nil {
		return e.Error()
	}
	return ""
}
func (p *Policy) Flush() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.persistLocked()
}
func (p *Policy) WaitHealthy(ctx context.Context) error {
	if p == nil {
		return nil
	}
	for {
		p.mu.RLock()
		unhealthy, changed := p.storeError != nil, p.healthChanged
		p.mu.RUnlock()
		if !unhealthy {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}
