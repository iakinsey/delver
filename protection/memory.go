package protection

import (
	"context"
	"fmt"
)

func (p *Policy) Reserve(ctx context.Context, n int64) (func(), error) {
	for {
		p.mu.Lock()
		budget := int64(p.state.InspectionMiB) << 20
		if n < 0 || n > budget {
			p.mu.Unlock()
			return nil, fmt.Errorf("response limit exceeds protection inspection memory budget")
		}
		if p.memoryUsed+n <= budget {
			p.memoryUsed += n
			p.mu.Unlock()
			return func() {
				p.mu.Lock()
				p.memoryUsed -= n
				close(p.memoryChanged)
				p.memoryChanged = make(chan struct{})
				p.mu.Unlock()
			}, nil
		}
		changed := p.memoryChanged
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		}
	}
}
func (p *Policy) SetMemory(revision uint64, mib int) error {
	if mib < 32 || mib > 2048 {
		return fmt.Errorf("inspection memory must be between 32 and 2048 MiB")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state.Revision != revision {
		return fmt.Errorf("protection revision conflict")
	}
	old := p.state
	p.state.InspectionMiB = mib
	p.state.Revision++
	if e := p.persistLocked(); e != nil {
		p.state = old
		return e
	}
	close(p.memoryChanged)
	p.memoryChanged = make(chan struct{})
	return nil
}

func FitsResponse(limit int64, mib int) bool {
	return limit > 0 && limit <= ((int64(mib)<<20)-(64<<10))/2
}
func (p *Policy) CanInspect(limit int64) bool {
	if p == nil {
		return true
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return FitsResponse(limit, p.state.InspectionMiB)
}
