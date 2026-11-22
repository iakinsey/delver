package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/flow"
	"os"
	"path/filepath"
)

type WorkspaceSettings struct {
	Revision  uint64               `json:"revision"`
	Execution flow.ExecutionLimits `json:"execution"`
}
type workspaceRegistry struct {
	Version  int                     `json:"schema_version"`
	Settings WorkspaceSettings       `json:"settings"`
	Crawls   map[string]*CrawlRecord `json:"crawls"`
}

func (w *Workspace) loadRegistry(data []byte) error {
	var envelope struct {
		Version int `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	if envelope.Version != 0 {
		if envelope.Version != 2 {
			return fmt.Errorf("unsupported workspace schema %d", envelope.Version)
		}
		var registry workspaceRegistry
		if err := json.Unmarshal(data, &registry); err != nil {
			return err
		}
		if registry.Settings.Revision == 0 {
			return fmt.Errorf("invalid workspace settings revision")
		}
		if err := registry.Settings.Execution.Validate(); err != nil {
			return err
		}
		w.settings = registry.Settings
		w.records = registry.Crawls
		return nil
	}
	// Retain an exact private copy before converting old registry/state formats.
	backup := filepath.Join(w.root, "migration-backups")
	if err := os.MkdirAll(backup, 0700); err != nil {
		return err
	}
	file := filepath.Join(backup, "crawls-v1.json")
	if f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600); err == nil {
		_, e := f.Write(data)
		if e == nil {
			e = f.Sync()
		}
		f.Close()
		if e != nil {
			return e
		}
	} else if !os.IsExist(err) {
		return err
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	seen := map[string]bool{}
	// Import policy declarations only from saved definitions and pipeline config.
	// Runtime summaries also contain a "fetch" object, but its counters are not
	// execution settings (in particular, historical process_limit may be zero).
	collect := func(value interface{}) error {
		if v, ok := value.(map[string]interface{}); ok {
			if fetch, ok := v["fetch"].(map[string]interface{}); ok {
				for _, key := range []string{"process_limit", "process_origin_limit", "process_origin_delay"} {
					if item, ok := fetch[key]; ok {
						switch key {
						case "process_limit":
							n, ok := item.(float64)
							if !ok || n != float64(int(n)) {
								return fmt.Errorf("invalid legacy process limit")
							}
							if !seen[key] || int(n) < w.settings.Execution.Concurrency {
								w.settings.Execution.Concurrency = int(n)
							}
						case "process_origin_limit":
							n, ok := item.(float64)
							if !ok || n != float64(int(n)) {
								return fmt.Errorf("invalid legacy host limit")
							}
							if !seen[key] || int(n) < w.settings.Execution.HostConcurrency {
								w.settings.Execution.HostConcurrency = int(n)
							}
						case "process_origin_delay":
							b, _ := json.Marshal(item)
							var d flow.Duration
							if err := json.Unmarshal(b, &d); err != nil {
								return err
							}
							if !seen[key] || d > w.settings.Execution.HostDelay {
								w.settings.Execution.HostDelay = d
							}
						}
						seen[key] = true
					}
				}
			}
		}
		return nil
	}
	collectConfiguration := func(record map[string]interface{}) error {
		for _, key := range []string{"definition", "draft"} {
			if err := collect(record[key]); err != nil {
				return err
			}
		}
		for _, key := range []string{"expert", "expert_draft"} {
			if pipeline, ok := record[key].(map[string]interface{}); ok {
				if err := collect(pipeline["config"]); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, value := range raw {
		if record, ok := value.(map[string]interface{}); ok {
			if err := collectConfiguration(record); err != nil {
				return err
			}
			if runs, ok := record["runs"].([]interface{}); ok {
				for _, value := range runs {
					if run, ok := value.(map[string]interface{}); ok {
						if err := collectConfiguration(run); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	if err := w.settings.Execution.Validate(); err != nil {
		return err
	}
	if err := json.Unmarshal(data, &w.records); err != nil {
		return err
	}
	w.dirty = true
	return nil
}
func (w *Workspace) HandleWorkspace(ctx context.Context, action string, raw json.RawMessage) (interface{}, error) {
	if action == "protection" {
		return w.handleProtection(ctx, raw)
	}
	if action != "settings" {
		return nil, fmt.Errorf("unknown workspace action")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var request struct {
		Revision  uint64                `json:"revision"`
		Execution *flow.ExecutionLimits `json:"execution"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, err
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if request.Execution == nil {
		return w.settings, nil
	}
	if w.closing {
		return nil, fmt.Errorf("workspace is shutting down")
	}
	if request.Revision != w.settings.Revision {
		return nil, fmt.Errorf("workspace revision conflict")
	}
	if err := request.Execution.Validate(); err != nil {
		return nil, err
	}
	previous := w.settings
	w.settings.Execution = *request.Execution
	w.settings.Revision++
	if err := w.saveLocked(); err != nil {
		w.settings = previous
		return nil, err
	}
	if err := w.broker.SetLimits(w.settings.Execution); err != nil {
		return nil, err
	}
	w.gate.SetLimits(w.settings.Execution.HostConcurrency, w.settings.Execution.HostDelay.Value())
	return w.settings, nil
}
