package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/frontier"
)

func (w *Workspace) handleTraps(ctx context.Context, r *CrawlRecord, slot *crawlSlot, action string, raw json.RawMessage) (interface{}, error) {
	var req struct {
		RunID string `json:"run_id"`
		Site  string `json:"site"`
	}
	if e := json.Unmarshal(raw, &req); e != nil {
		return nil, e
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	w.mu.Lock()
	runs := append([]CrawlRun(nil), r.Runs...)
	w.mu.Unlock()
	var selected *CrawlRun
	for _, run := range runs {
		if req.RunID == "" || run.ID == req.RunID {
			copy := run
			selected = &copy
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("run required")
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
		a, e := prepareRunPipeline(w.runRoot(r.ID, selected.ID), *selected)
		if e != nil {
			return nil, e
		}
		for _, res := range a.Resources {
			if res.Type == "frontier_store" {
				var p frontier.StoreParams
				if e = json.Unmarshal(res.Parameters, &p); e != nil {
					return nil, e
				}
				store, e = frontier.OpenStore(p)
				if e != nil {
					return nil, e
				}
				store.SetProtection(w.protection)
				defer store.Close()
				break
			}
		}
	}
	if store == nil {
		return nil, fmt.Errorf("crawl has no supported frontier")
	}
	if action == "trap_restore" {
		if req.Site == "" {
			return nil, fmt.Errorf("site required")
		}
		if e := store.RestoreTrap(ctx, req.Site); e != nil {
			return nil, e
		}
		if e := store.DrainParked(ctx, 128); e != nil {
			return nil, e
		}
	}
	return store.TrapDecisions(ctx)
}
