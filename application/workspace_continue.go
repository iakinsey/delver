package application

import (
	"encoding/json"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/types/errs"
	"time"
)

func effectiveTermination(run CrawlRun) Termination {
	if run.Continued > 0 {
		return Termination{WhenDrained: true}
	}
	return run.Definition.Termination
}
func continuationPipeline(a config.Application) (config.Application, error) {
	hasFrontier := false
	for i, r := range a.Resources {
		if r.Type == "frontier_store" {
			hasFrontier = true
			var p map[string]json.RawMessage
			if err := json.Unmarshal(r.Parameters, &p); err != nil {
				return a, err
			}
			p["max_dispatches"] = json.RawMessage("0")
			p["revisit_after"] = json.RawMessage(`"never"`)
			data, _ := json.Marshal(p)
			a.Resources[i].Parameters = data
		}
	}
	if !hasFrontier {
		return a, errs.NewRequestError("drain requires a frontier-based pipeline")
	}
	timers := map[string]bool{}
	for _, r := range a.Resources {
		if r.Type == "timer" {
			timers[r.Name] = true
		}
	}
	workers := []config.Worker{}
	used := map[string]bool{}
	for _, worker := range a.Workers {
		if worker.Manager == "job" && worker.Type != "frontier_publisher" && worker.Type != "frontier_cleanup" || timers[worker.Inbox] {
			continue
		}
		workers = append(workers, worker)
		used[worker.Inbox] = true
		used[worker.Outbox] = true
	}
	a.Workers = workers
	resources := []config.Resource{}
	for _, r := range a.Resources {
		if timers[r.Name] && !used[r.Name] {
			continue
		}
		resources = append(resources, r)
	}
	a.Resources = resources
	return a, Validate(a)
}
func (w *Workspace) continueLatest(r *CrawlRecord, slot *crawlSlot, revision uint64) (interface{}, error) {
	w.mu.Lock()
	if r.Archived || revision != r.Revision || len(r.Runs) == 0 {
		w.mu.Unlock()
		return nil, errs.NewRequestError("stopped run with pending work required")
	}
	run := r.Runs[len(r.Runs)-1]
	w.mu.Unlock()
	probeRun := run
	probeRun.Continued++
	if _, err := prepareRunPipeline(w.runRoot(r.ID, run.ID), probeRun); err != nil {
		return nil, err
	}
	if slot.supervisor != nil {
		s := slot.supervisor
		s.mu.Lock()
		state := s.status
		s.mu.Unlock()
		if state != "stopped" && state != "failed" {
			return nil, errs.NewRequestError("drain requires a stopped run")
		}
		if err := s.Close(); err != nil {
			return nil, err
		}
		slot.setSupervisor(nil)
	}
	run.Continued++
	run.State = "starting"
	w.mu.Lock()
	previousRun, previousRevision := r.Runs[len(r.Runs)-1], r.Revision
	r.Revision++
	r.Runs[len(r.Runs)-1] = run
	slot.clockRunning = false
	slot.clockMark = time.Now()
	err := w.saveLocked()
	w.mu.Unlock()
	if err != nil {
		w.mu.Lock()
		r.Runs[len(r.Runs)-1], r.Revision = previousRun, previousRevision
		w.mu.Unlock()
		return nil, err
	}
	s, err := w.openSupervisor(r, run)
	if err != nil {
		w.mu.Lock()
		r.Runs[len(r.Runs)-1].State = "failed"
		w.saveLocked()
		w.mu.Unlock()
		return nil, err
	}
	slot.setSupervisor(s)
	slot.draining = false
	slot.stable = 0
	s.mu.Lock()
	err = s.setDesired("running")
	if err == nil {
		err = s.start()
	}
	s.mu.Unlock()
	return map[string]string{"run_id": run.ID}, err
}
func sanitizeRuntimeSnapshot(snapshot RuntimeSnapshot, a config.Application) RuntimeSnapshot {
	for name, activity := range snapshot.Workers {
		activity.LastError = sanitizeText(activity.LastError, a)
		snapshot.Workers[name] = activity
	}
	for name, value := range snapshot.Health {
		snapshot.Health[name] = sanitizeText(value, a)
	}
	return snapshot
}
func (w *Workspace) saveRunSummary(id, runID string, snapshot RuntimeSnapshot) {
	w.mu.Lock()
	defer w.mu.Unlock()
	r := w.records[id]
	if r != nil && len(r.Runs) > 0 && r.Runs[len(r.Runs)-1].ID == runID {
		r.Runs[len(r.Runs)-1].Summary = &snapshot
		w.dirty = true
	}
}
