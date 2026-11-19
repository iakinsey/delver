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
func (w *Workspace) acceptWorkflow(r *CrawlRecord, slot *crawlSlot, action string, revision uint64, id string, details ...Operation) (interface{}, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closing || w.ctx.Err() != nil {
		return nil, errs.NewRequestError("workspace is shutting down")
	}
	if id == "" {
		id = string(types.NewV4())
	}
	runID := ""
	key := id
	if len(details) > 0 && len(r.Runs) > 0 {
		runID = r.Runs[len(r.Runs)-1].ID
		key = runID + ":" + id
	}
	if existing := r.Operations[key]; existing != nil {
		queue, record := "", ""
		if len(details) > 0 {
			queue, record = details[0].Queue, details[0].Record
		}
		// Run-scoped commands do not include a draft revision in their
		// request signature. Saving a draft must not invalidate a retry.
		revisionChanged := len(details) == 0 && existing.Version != revision
		if existing.Action != action || revisionChanged || existing.Queue != queue || existing.Record != record {
			return nil, errs.NewRequestError("operation ID was already used for another request")
		}
		copy := *existing
		return copy, nil
	}
	if activeWorkflow(r) {
		return nil, errs.NewRequestError("crawl operation in progress")
	}
	if revision != r.Revision {
		return nil, errs.NewRequestError("crawl revision conflict")
	}
	if r.Deletion != nil {
		return nil, errs.NewRequestError("crawl deletion is in progress")
	}
	if r.Archived {
		return nil, errs.NewRequestError("crawl archived")
	}
	if r.Operations == nil {
		r.Operations = map[string]*Operation{}
	}
	op := &Operation{RunID: runID, ID: id, Action: action, Version: revision, State: "accepted", Updated: time.Now().Unix()}
	if len(details) > 0 {
		op.Queue = details[0].Queue
		op.Record = details[0].Record
	}
	r.Operations[key] = op
	if err := w.saveLocked(); err != nil {
		delete(r.Operations, key)
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
				err = w.executeWorkflow(r, slot, action, revision, op)
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
	return out, nil
}

// All accepted commands execute under the same per-crawl slot lock.
func (w *Workspace) executeWorkflow(r *CrawlRecord, slot *crawlSlot, action string, revision uint64, op *Operation) error {
	switch action {
	case "start_run", "activate", "continue_run":
		_, err := w.applyRun(r, slot, action, revision)
		return err
	case "start", "resume":
		w.mu.Lock()
		reopen := r.Draft != nil || r.ExpertDraft != nil || len(r.Runs) == 0 || r.Runs[len(r.Runs)-1].State == "completed" || slot.supervisor == nil
		w.mu.Unlock()
		if reopen {
			_, err := w.applyRun(r, slot, "activate", revision)
			return err
		}
	}
	if slot.supervisor == nil {
		return errs.NewRequestError("run required")
	}
	return slot.supervisor.Execute(w.ctx, action, op.Queue, op.Record)
}
