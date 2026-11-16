package flow

import (
	"context"
	"fmt"
	"runtime"
	"time"
)

// ExecutionLimits belongs to the workspace, not to an individual crawl.
type ExecutionLimits struct {
	Concurrency     int      `json:"concurrency"`
	HostConcurrency int      `json:"host_concurrency"`
	HostDelay       Duration `json:"host_delay"`
}

func DefaultExecutionLimits() ExecutionLimits {
	return ExecutionLimits{runtime.NumCPU() * 16, 2, Duration(100 * time.Millisecond)}
}
func (v ExecutionLimits) Validate() error {
	if v.Concurrency < 1 || v.Concurrency > 8192 {
		return fmt.Errorf("process concurrency must be between 1 and 8192")
	}
	if v.HostConcurrency < 1 || v.HostConcurrency > 8192 {
		return fmt.Errorf("shared host concurrency must be between 1 and 8192")
	}
	if v.HostDelay < 0 || v.HostDelay > Duration(24*time.Hour) {
		return fmt.Errorf("shared host delay must be between 0s and 24h")
	}
	return nil
}

type Broker = broker

func NewBroker(limits ExecutionLimits) *Broker {
	return &broker{limits: limits, registered: map[string]*Controller{}, active: map[string]int{}}
}
func (b *broker) Limits() ExecutionLimits { b.mu.Lock(); defer b.mu.Unlock(); return b.limits }
func (b *broker) SetLimits(v ExecutionLimits) error {
	if err := v.Validate(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.limits = v
	b.dispatch()
	return nil
}

type brokerKey struct{}

func WithBroker(ctx context.Context, b *Broker) context.Context {
	return context.WithValue(ctx, brokerKey{}, b)
}
func brokerFrom(ctx context.Context) *broker {
	if b, ok := ctx.Value(brokerKey{}).(*broker); ok {
		return b
	}
	return &global
}
