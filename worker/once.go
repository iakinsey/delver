package worker

import (
	"context"
	"fmt"
	"github.com/iakinsey/delver/resource/maps"
	"github.com/iakinsey/delver/types"
	"sync"
)

// OnceWorker persists successful source admission across runtime restarts.
type OnceWorker struct {
	Worker
	Store     maps.Map
	Key       string
	healthMu  sync.RWMutex
	lastError string
}

func (w *OnceWorker) OnMessage(m types.Message) (interface{}, error) {
	return w.OnMessageContext(context.Background(), m)
}
func (w *OnceWorker) OnMessageContext(ctx context.Context, m types.Message) (interface{}, error) {
	if value, err := w.Store.Get([]byte(w.Key)); err == nil && len(value) > 0 {
		return nil, nil
	} else if err != nil && err != maps.ErrKeyNotFound {
		return nil, err
	}
	var result interface{}
	var err error
	if target, ok := w.Worker.(ContextWorker); ok {
		result, err = target.OnMessageContext(ctx, m)
	} else {
		result, err = w.Worker.OnMessage(m)
	}
	if err == nil {
		if h, ok := w.Worker.(interface{ Health() string }); ok && h.Health() != "" {
			err = fmt.Errorf("source incomplete: %s", h.Health())
		}
	}
	w.healthMu.Lock()
	w.lastError = ""
	if err != nil {
		w.lastError = err.Error()
	}
	w.healthMu.Unlock()
	return result, err
}
func (w *OnceWorker) OnCommit(m types.Message) error {
	if c, ok := w.Worker.(MessageCommitter); ok {
		if err := c.OnCommit(m); err != nil {
			return err
		}
	}
	return w.Store.Set([]byte(w.Key), []byte{1})
}

// Health remains visible while a one-shot source retries incomplete input.
func (w *OnceWorker) Health() string {
	if source, ok := w.Worker.(interface{ Health() string }); ok {
		return source.Health()
	}
	w.healthMu.RLock()
	defer w.healthMu.RUnlock()
	return w.lastError
}

func (w *OnceWorker) PurgeMatching(ctx context.Context, match func(string) bool) error {
	if p, ok := w.Worker.(interface {
		PurgeMatching(context.Context, func(string) bool) error
	}); ok {
		return p.PurgeMatching(ctx, match)
	}
	return nil
}

func (w *OnceWorker) VerifyNoMatches(ctx context.Context, match func(string) bool) error {
	if v, ok := w.Worker.(interface {
		VerifyNoMatches(context.Context, func(string) bool) error
	}); ok {
		return v.VerifyNoMatches(ctx, match)
	}
	return nil
}
