package application

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// importRunState reads legacy supervisor journals only when the registry lacks desired state.
func (w *Workspace) importRunState() error {
	for _, record := range w.records {
		if record == nil {
			return fmt.Errorf("invalid crawl record")
		}
		for i := range record.Runs {
			run := &record.Runs[i]
			if run.Desired != "" {
				continue
			}
			run.Desired = "stopped"
			path := filepath.Join(w.runRoot(record.ID, run.ID), "state.json")
			if run.Legacy && run.Expert != nil && run.Expert.Config.API.OperationsPath != "" {
				path = run.Expert.Config.API.OperationsPath
			}
			data, err := os.ReadFile(path)
			if err == nil {
				var old operationsState
				if err = json.Unmarshal(data, &old); err != nil {
					return fmt.Errorf("legacy run state %s: %w", run.ID, err)
				}
				if old.Desired != "running" && old.Desired != "paused" && old.Desired != "stopped" {
					return fmt.Errorf("invalid legacy desired state")
				}
				run.Desired = old.Desired
				// A legacy supervisor could hold an unactivated draft and a
				// higher configuration revision outside the workspace registry.
				if run.Legacy && old.Version > run.Version {
					run.Version = old.Version
					if old.Version > record.Revision {
						record.Revision = old.Version
					}
				}
				if i == len(record.Runs)-1 && old.Draft != nil && record.Draft == nil && record.ExpertDraft == nil {
					if err := Validate(*old.Draft); err != nil {
						return fmt.Errorf("legacy draft migration: %w", err)
					}
					definition := record.Definition
					record.Draft = &definition
					record.ExpertDraft = old.Draft
				}
				if record.Operations == nil {
					record.Operations = map[string]*Operation{}
				}
				for id, op := range old.Operations {
					if op == nil || op.ID != id {
						return fmt.Errorf("invalid legacy operation")
					}
					copy := *op
					copy.RunID = run.ID
					record.Operations[run.ID+":"+id] = &copy
				}
				dir := filepath.Join(w.root, "migration-backups")
				if err := os.MkdirAll(dir, 0700); err != nil {
					return err
				}
				backup := filepath.Join(dir, record.ID+"-"+run.ID+"-state.json")
				f, e := os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if e == nil {
					_, e = f.Write(data)
					if e == nil {
						e = f.Sync()
					}
					f.Close()
					if e != nil {
						return e
					}
				} else if !os.IsExist(e) {
					return e
				}
			} else if !os.IsNotExist(err) {
				return err
			} else {
				switch run.State {
				case "running", "starting", "draining":
					run.Desired = "running"
				case "paused", "pausing":
					run.Desired = "paused"
				}
			}
			w.dirty = true
		}
	}
	return nil
}
