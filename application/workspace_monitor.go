package application

import (
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/resource/maps"
	"github.com/iakinsey/delver/worker"
	"time"
)

func (w *Workspace) monitor() {
	defer close(w.done)
	lastFlush := time.Now()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-w.ctx.Done():
			w.checks.Wait()
			return
		case <-ticker.C:
			if time.Since(lastFlush) >= time.Second {
				w.mu.Lock()
				if w.dirty {
					w.saveLocked()
				}
				w.mu.Unlock()
				lastFlush = time.Now()
			}
			w.mu.Lock()
			slots := map[string]*crawlSlot{}
			for id, s := range w.slots {
				slots[id] = s
			}
			w.mu.Unlock()
			for id, slot := range slots {
				select {
				case slot.inspect <- struct{}{}:
					w.checks.Add(1)
					go func(id string, s *crawlSlot) {
						defer w.checks.Done()
						defer func() { <-s.inspect }()
						w.checkRun(id, s)
					}(id, slot)
				default:
				}
			}
		}
	}
}
func (w *Workspace) checkRun(id string, slot *crawlSlot) {
	slot.mu.Lock()
	defer slot.mu.Unlock()
	s := slot.supervisor
	if s == nil {
		return
	}
	w.mu.Lock()
	record := w.records[id]
	if record == nil || len(record.Runs) == 0 {
		w.mu.Unlock()
		return
	}
	w.accountTimeLocked(record, slot, time.Now())
	run := record.Runs[len(record.Runs)-1]
	w.mu.Unlock()
	if run.State == "completed" {
		return
	}
	s.mu.Lock()
	status, control := s.status, s.control
	s.mu.Unlock()

	if status != "running" {
		w.mu.Lock()
		if len(record.Runs) > 0 && record.Runs[len(record.Runs)-1].ID == run.ID && record.Runs[len(record.Runs)-1].State != status {
			record.Runs[len(record.Runs)-1].State = status
			w.saveLocked()
		}
		w.mu.Unlock()
		return
	}
	if !slot.draining {
		run.State = "running"
	}

	if control == nil {
		return
	}
	snapshot := control.snapshot()
	front := snapshot.Frontier
	budget := effectiveTermination(run).MaxRequests > 0 && front["dispatched"] >= effectiveTermination(run).MaxRequests
	duration := false
	if raw := effectiveTermination(run).Duration; raw != "" {
		limit, _ := time.ParseDuration(raw)
		duration = time.Duration(run.Elapsed) >= limit
	}
	if budget || duration {
		slot.draining = true
	}
	if slot.draining {
		control.mu.Lock()
		if control.prepared != nil {
			for _, d := range control.prepared.workerDefinitions() {
				if d.Manager == "job" && d.Type != "frontier_cleanup" {
					control.prepared.workers[d.Name].(worker.Pausable).Pause(true)
				}
			}
		}
		control.mu.Unlock()
		run.State = "draining"
	}
	sourceDone := true
	if effectiveTermination(run).WhenDrained {
		control.mu.Lock()
		if control.prepared == nil {
			sourceDone = false
		} else {
			p := control.prepared
			store, _ := p.resources["source_progress"].(maps.Map)
			for _, d := range p.workerDefinitions() {
				if d.Once || d.Type == "fixed_seed_publisher" || d.Type == "rss_feed_publisher" || d.Type == "dfs_basic_publisher" {
					if !d.Once || store == nil {
						sourceDone = false
						break
					}
					if value, err := store.Get([]byte(d.Name)); err != nil || len(value) == 0 {
						sourceDone = false
						break
					}
				}
			}
		}
		control.mu.Unlock()
	}
	idle := front["reserved"] == 0
	for _, n := range snapshot.Queues {
		if n != 0 {
			idle = false
		}
	}
	control.mu.Lock()
	var definitions []config.Worker
	if control.prepared != nil {
		definitions = control.prepared.workerDefinitions()
	}
	control.mu.Unlock()
	idle = idle && crawlWorkersIdle(snapshot.Workers, definitions)
	for _, counts := range snapshot.QueueStates {
		if counts["failed"] > 0 {
			idle = false
		}
	}
	if !slot.draining {
		idle = idle && effectiveTermination(run).WhenDrained && sourceDone && front["pending_and_active"] == 0 && front["deferred_discoveries"] == 0
	}
	if idle {
		slot.stable++
	} else {
		slot.stable = 0
	}
	if slot.stable >= 3 {
		if err := stopSupervisor(s); err != nil {
			s.mu.Lock()
			s.lastError = err.Error()
			s.mu.Unlock()
			return
		}
		run.State = "completed"
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(record.Runs) > 0 && record.Runs[len(record.Runs)-1].ID == run.ID {
		prior := record.Runs[len(record.Runs)-1].State
		record.Runs[len(record.Runs)-1].State = run.State
		if prior != run.State {
			w.saveLocked()
		}
	}
}

// Periodic frontier jobs may be active even when all crawl work is drained.
// Frontier reservations, pending records, and queue leases guard their output;
// shutdown still waits for these jobs before closing storage.
func crawlWorkersIdle(activities map[string]worker.Activity, definitions []config.Worker) bool {
	for name, activity := range activities {
		if activity.Active == 0 {
			continue
		}
		maintenance := false
		for _, definition := range definitions {
			if definition.Name == name && definition.Manager == "job" && (definition.Type == "frontier_publisher" || definition.Type == "frontier_cleanup") {
				maintenance = true
				break
			}
		}
		if !maintenance {
			return false
		}
	}
	return true
}
