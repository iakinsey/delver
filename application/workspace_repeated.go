package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/report"
	"os"
)

// The caller owns slot.mu. A live store is borrowed under the existing storage
// lock; historical runs open their existing frontier, never a new crawl.
func (w *Workspace) withRepeatedStore(ctx context.Context, r *CrawlRecord, slot *crawlSlot, runID string, fn func(*frontier.Store, *report.Store, CrawlRun) error) error {
	w.mu.Lock()
	var selected *CrawlRun
	if w.records[r.ID] != r || r.Deletion != nil {
		w.mu.Unlock()
		return fmt.Errorf("crawl unavailable")
	}
	for _, run := range r.Runs {
		if runID == "" || run.ID == runID {
			copy := run
			selected = &copy
		}
	}
	w.mu.Unlock()
	if selected == nil {
		return fmt.Errorf("run required")
	}
	a, err := prepareRunPipeline(w.runRoot(r.ID, selected.ID), *selected)
	if err != nil {
		return err
	}
	var store *frontier.Store
	if slot.supervisor != nil && slot.supervisor.runID == selected.ID && slot.supervisor.control != nil {
		c := slot.supervisor.control
		c.storageMu.RLock()
		defer c.storageMu.RUnlock()
		c.mu.Lock()
		if c.prepared != nil {
			for _, resource := range c.prepared.resources {
				if f, ok := resource.(*frontier.Store); ok {
					store = f
					break
				}
			}
		}
		c.mu.Unlock()
	}
	if store == nil {
		for _, resource := range a.Resources {
			if resource.Type != "frontier_store" {
				continue
			}
			var p frontier.StoreParams
			if err = json.Unmarshal(resource.Parameters, &p); err != nil {
				return err
			}
			store, err = frontier.OpenStore(p)
			if err != nil {
				return err
			}
			store.SetProtection(w.protection)
			defer store.Close()
			break
		}
	}
	if store == nil {
		return fmt.Errorf("crawl has no supported frontier")
	}
	path := a.Config.API.OperationsPath + ".report.sqlite"
	_, statErr := os.Stat(path)
	ledger, err := report.Open(path, os.IsNotExist(statErr))
	if err != nil {
		return err
	}
	defer ledger.Close()
	ledger.SetProtection(w.protection)
	return fn(store, ledger, *selected)
}

type repeatedRequest struct {
	OperationID string `json:"operation_id"`
	RunID       string `json:"run_id"`
	Family      string `json:"family"`
	Exempt      bool   `json:"exempt"`
	Query       string `json:"query"`
	After       string `json:"after"`
	Limit       int    `json:"limit"`
	Revision    uint64 `json:"revision"`
	ID          string `json:"id"`
	Resume      string `json:"resume"`
}

func (w *Workspace) handleRepeated(ctx context.Context, r *CrawlRecord, slot *crawlSlot, action string, raw json.RawMessage) (interface{}, error) {
	var req repeatedRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if action == "repeated_override" || action == "repeated_analyze" {
		if action == "repeated_override" && (len(req.Family) != 64) {
			return nil, fmt.Errorf("family ID required")
		}
		input, _ := json.Marshal(req)
		return w.acceptWorkflow(r, slot, action, req.Revision, req.ID, Operation{Input: input})
	}
	if action == "repeated_cancel" {
		w.mu.Lock()
		defer w.mu.Unlock()
		if req.Revision != r.Revision {
			return nil, fmt.Errorf("crawl revision conflict")
		}
		if req.OperationID == "" {
			return nil, fmt.Errorf("analysis operation ID required")
		}
		cancelled := false
		for _, op := range r.Operations {
			if op.ID == req.OperationID && op.Action == "repeated_analyze" && (op.State == "accepted" || op.State == "running") {
				op.CancelRequested = true
				cancelled = true
			}
		}
		if cancelled {
			if job := w.repeatedJobs[r.ID]; job != nil {
				job()
			}
			if err := w.saveLocked(); err != nil {
				return nil, err
			}
		}
		return map[string]bool{"cancelled": cancelled}, nil
	}

	slot.mu.Lock()
	defer slot.mu.Unlock()
	var out interface{}
	err := w.withRepeatedStore(ctx, r, slot, req.RunID, func(store *frontier.Store, ledger *report.Store, _ CrawlRun) error {
		if action == "repeated_members" {
			v, err := store.RepeatedMembers(ctx, req.Family, req.Query, req.After, req.Limit)
			out = v
			return err
		}
		v, err := store.RepeatedPages(ctx, req.Query, req.After, req.Limit)
		out = v
		return err
	})
	return out, err
}

func (w *Workspace) executeRepeatedOverride(ctx context.Context, r *CrawlRecord, slot *crawlSlot, op *Operation) error {
	var req repeatedRequest
	if err := json.Unmarshal(op.Input, &req); err != nil {
		return err
	}
	target, projectionTarget := int64(0), int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		done := false
		slot.mu.Lock()
		err := w.withRepeatedStore(ctx, r, slot, req.RunID, func(store *frontier.Store, ledger *report.Store, _ CrawlRun) error {
			if err := store.SetFamilyExempt(ctx, req.Family, req.Exempt); err != nil {
				return err
			}
			if err := store.MaintainRepeatedRouting(ctx, 128); err != nil {
				return err
			}
			v, err := store.RepeatedPages(ctx, "", "", 1)
			if err != nil {
				return err
			}
			if target == 0 {
				target = v.Routing.Target
				projectionTarget = v.Revision
				w.mu.Lock()
				op.Progress = map[string]int64{"routing_target": target, "projection_target": projectionTarget}
				err = w.saveLocked()
				w.mu.Unlock()
				if err != nil {
					return err
				}
			}
			if err := store.FlushRepeated(ctx, ledger); err != nil {
				return err
			}
			applied, err := ledger.RepeatedRevision(ctx)
			if err != nil {
				return err
			}
			done = v.Routing.Completed >= target && applied >= projectionTarget
			return nil
		})
		slot.mu.Unlock()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}
