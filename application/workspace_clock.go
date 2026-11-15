package application

import "time"

// Active time is accounted at state transitions, not inferred from sparse UI
// polls. Restart downtime and paused time never consume the duration budget.
func (w *Workspace) accountTimeLocked(record *CrawlRecord, slot *crawlSlot, now time.Time) {
	if slot.clockRunning && !slot.clockMark.IsZero() && len(record.Runs) > 0 {
		delta := now.Sub(slot.clockMark)
		if delta > 0 {
			record.Runs[len(record.Runs)-1].Elapsed += int64(delta)
			w.dirty = true
		}
	}
	slot.clockMark = now
}
func (w *Workspace) onRunStatus(id, runID, state string, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	record, slot := w.records[id], w.slots[id]
	if record == nil || slot == nil || len(record.Runs) == 0 {
		return
	}
	run := &record.Runs[len(record.Runs)-1]
	if run.ID != runID || run.State == "completed" {
		return
	}
	w.accountTimeLocked(record, slot, now)
	slot.clockRunning = state == "running"
	run.State = state
	w.saveLocked()
}
