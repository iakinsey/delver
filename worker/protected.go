package worker

import (
	"context"
	"errors"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/types"
)

// ProtectedWorker discards late deliveries without retrying them. Sinks also
// fence their writes with the same policy lock used to activate exclusions.
type ProtectedWorker struct {
	Worker Worker
	Policy *protection.Policy
	Sink   bool
}

func (w *ProtectedWorker) OnMessage(m types.Message) (interface{}, error) {
	return w.OnMessageContext(context.Background(), m)
}
func (w *ProtectedWorker) OnMessageContext(ctx context.Context, m types.Message) (out interface{}, err error) {
	if e := w.Policy.WaitHealthy(ctx); e != nil {
		return nil, e
	}
	if e := w.Policy.CheckPayload(m.Message); e != nil {
		if errors.Is(e, protection.ErrBlocked) {
			return nil, nil
		}
		return nil, e
	}
	run := func() error {
		if w.Sink {
			clean, e := w.Policy.SanitizePayload(m.Message)
			if e != nil {
				return e
			}
			if clean == nil && len(m.Message) > 0 {
				return nil
			}
			m.Message = clean
		}

		if c, ok := w.Worker.(ContextWorker); ok {
			out, err = c.OnMessageContext(ctx, m)
		} else {
			out, err = w.Worker.OnMessage(m)
		}
		return err
	}
	if w.Sink {
		err = w.Policy.GuardPayload(m.Message, run)
	} else {
		err = run()
	}
	if e := w.Policy.WaitHealthy(ctx); e != nil {
		return nil, e
	}
	if errors.Is(err, protection.ErrBlocked) || errors.Is(w.Policy.CheckPayload(m.Message), protection.ErrBlocked) {
		return nil, nil
	}
	return out, err
}
func (w *ProtectedWorker) OnComplete() { w.Worker.OnComplete() }
func (w *ProtectedWorker) OnCommit(m types.Message) error {
	if c, ok := w.Worker.(MessageCommitter); ok {
		return c.OnCommit(m)
	}
	return nil
}
func (w *ProtectedWorker) PurgeMatching(ctx context.Context, match func(string) bool) error {
	if v, ok := w.Worker.(interface {
		PurgeMatching(context.Context, func(string) bool) error
	}); ok {
		return v.PurgeMatching(ctx, match)
	}
	return nil
}

type protectedHealthWorker struct{ *ProtectedWorker }

func (w *protectedHealthWorker) Health() string { return w.Worker.(HealthSource).Health() }

func Protect(w Worker, p *protection.Policy, sink bool) Worker {
	protected := &ProtectedWorker{Worker: w, Policy: p, Sink: sink}
	if _, ok := w.(HealthSource); ok {
		return &protectedHealthWorker{protected}
	}
	return protected
}

// Protected durable deliveries are owned by the manager until acknowledgement.
func (w *ProtectedWorker) DiscardDelivery(m types.Message, outputs []types.Message) {
	clear(m.Message)
	for _, output := range outputs {
		clear(output.Message)
	}
}

func (w *ProtectedWorker) VerifyNoMatches(ctx context.Context, match func(string) bool) error {
	if v, ok := w.Worker.(interface {
		VerifyNoMatches(context.Context, func(string) bool) error
	}); ok {
		return v.VerifyNoMatches(ctx, match)
	}
	return nil
}
