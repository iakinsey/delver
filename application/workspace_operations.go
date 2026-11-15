package application

import (
	"context"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/errs"
	"time"
)

func (s *crawlSlot) setSupervisor(supervisor *Supervisor) {
	s.runtimeMu.Lock()
	s.supervisor = supervisor
	s.initError = ""
	s.runtimeMu.Unlock()
}
func activeWorkflow(r *CrawlRecord) bool {
	for _, op := range r.Operations {
		if op.State == "accepted" || op.State == "running" {
			return true
		}
	}
	return false
}
func (w *Workspace) acceptWorkflow(r *CrawlRecord, slot *crawlSlot, action string, revision uint64, id string) (interface{}, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closing || w.ctx.Err() != nil {
		return nil, errs.NewRequestError("workspace is shutting down")
	}
	if id == "" {
		id = string(types.NewV4())
	}
	if existing := r.Operations[id]; existing != nil {
		if existing.Action != action || existing.Version != revision {
			return nil, errs.NewRequestError("operation ID was already used for another request")
		}
		copy := *existing
		return copy, nil
	}
	if activeWorkflow(r) {
		return nil, errs.NewRequestError("wait for the active crawl operation")
	}
	if revision != r.Revision {
		return nil, errs.NewRequestError("this crawl changed; reload before running")
	}
	if r.Archived {
		return nil, errs.NewRequestError("restore this crawl before running")
	}
	if r.Operations == nil {
		r.Operations = map[string]*Operation{}
	}
	op := &Operation{ID: id, Action: action, Version: revision, State: "accepted", Updated: time.Now().Unix()}
	r.Operations[id] = op
	if err := w.saveLocked(); err != nil {
		delete(r.Operations, id)
		return nil, err
	}
	w.background.Add(1)
	go func() {
		defer w.background.Done()
		slot.mu.Lock()
		defer slot.mu.Unlock()
		w.mu.Lock()
		op.State = "running"
		err := w.saveLocked()
		w.mu.Unlock()
		if err == nil {
			if w.ctx.Err() != nil {
				err = w.ctx.Err()
			} else {
				_, err = w.applyRun(r, slot, action, revision)
			}
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		op.State = "completed"
		if err != nil {
			op.State = "failed"
			op.Error = sanitizeText(err.Error(), configurationForSecrets(r.Definition))
			if r.Expert != nil {
				op.Error = sanitizeText(op.Error, *r.Expert)
			}
		}
		op.Updated = time.Now().Unix()
		if err := w.saveLocked(); err != nil {
			op.State = "failed"
			op.Error = "Could not persist operation completion: " + err.Error()
		}
	}()
	copy := *op
	return copy, nil
}
func (w *Workspace) crawlOperations(ctx context.Context, r *CrawlRecord, slot *crawlSlot) (interface{}, error) {
	out := map[string]interface{}{}
	w.mu.Lock()
	for id, op := range r.Operations {
		copy := *op
		out["workspace:"+id] = copy
	}
	w.mu.Unlock()
	slot.runtimeMu.RLock()
	s := slot.supervisor
	slot.runtimeMu.RUnlock()
	if s != nil {
		value, err := s.Handle(ctx, "operations", nil)
		if err != nil {
			return nil, err
		}
		for id, op := range value.(map[string]Operation) {
			out["run:"+id] = op
		}
	}
	return out, nil
}
