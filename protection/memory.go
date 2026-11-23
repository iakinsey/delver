package protection

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type InspectionMemory struct {
	ReservedBytes int64 `json:"reserved_bytes"`
	PeakBytes     int64 `json:"peak_bytes"`
	Active        int   `json:"active"`
	Waiters       int   `json:"waiters"`
	Waits         int64 `json:"waits"`
	WaitNS        int64 `json:"wait_ns"`
}

func (p *Policy) memorySnapshotLocked() InspectionMemory {
	return InspectionMemory{p.memoryUsed, p.memoryPeak, p.memoryActive, p.memoryWaiters, p.memoryWaits, p.memoryWaitNS}
}
func (p *Policy) MemorySnapshot() InspectionMemory {
	if p == nil {
		return InspectionMemory{}
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.memorySnapshotLocked()
}

func (p *Policy) Reserve(ctx context.Context, n int64) (func(), error) {
	started := time.Now()
	waiting := false
	defer func() {
		if waiting {
			p.mu.Lock()
			p.memoryWaiters--
			p.memoryWaitNS += time.Since(started).Nanoseconds()
			p.mu.Unlock()
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p.mu.Lock()
		budget := int64(p.state.InspectionMiB) << 20
		if n < 0 || n > budget {
			p.mu.Unlock()
			return nil, fmt.Errorf("response limit exceeds protection inspection memory budget")
		}
		if p.memoryUsed+n <= budget {
			p.memoryUsed += n
			p.memoryActive++
			p.memoryPeak = max(p.memoryPeak, p.memoryUsed)
			p.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					p.mu.Lock()
					p.memoryUsed -= n
					p.memoryActive--
					close(p.memoryChanged)
					p.memoryChanged = make(chan struct{})
					p.mu.Unlock()
				})
			}, nil
		}
		if !waiting {
			waiting = true
			p.memoryWaiters++
			p.memoryWaits++
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
