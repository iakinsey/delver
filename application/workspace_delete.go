package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/gateway"
	"github.com/iakinsey/delver/report"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/errs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type CrawlDeletion struct {
	PreserveLogs bool `json:"preserve_logs"`
}

func (w *Workspace) deletionPaths(r *CrawlRecord) ([]string, error) {
	paths := []string{filepath.Join(w.root, "crawls", r.ID)}
	for _, run := range r.Runs {
		if run.Legacy && run.Expert != nil {
			paths = append(paths, pipelinePaths(*run.Expert)...)
			if path := run.Expert.Config.API.OperationsPath; path != "" {
				paths = append(paths, path, path+".lock", path+".tmp", path+".fetch.json", path+".fetch.json.tmp", path+".report.sqlite", path+".report.sqlite-wal", path+".report.sqlite-shm")
			}
		}
	}
	for i, path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		paths[i] = absolute

		for probe := absolute; ; probe = filepath.Dir(probe) {
			if st, e := os.Lstat(probe); e == nil && st.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("refusing to delete storage through a symbolic link: %s", probe)
			}
			resolved, e := filepath.EvalSymlinks(probe)
			if e == nil {
				if resolved != probe {
					return nil, fmt.Errorf("refusing to delete storage through a symbolic link: %s", probe)
				}
				break
			}
			if !os.IsNotExist(e) {
				return nil, e
			}
			if filepath.Dir(probe) == probe {
				break
			}
		}
		legacyState := absolute == filepath.Join(w.root, "state.json") || absolute == filepath.Join(w.root, "state.json.lock") || absolute == filepath.Join(w.root, "state.json.tmp") || absolute == filepath.Join(w.root, "state.json.fetch.json") || absolute == filepath.Join(w.root, "state.json.fetch.json.tmp") || absolute == filepath.Join(w.root, "state.json.report.sqlite") || absolute == filepath.Join(w.root, "state.json.report.sqlite-wal") || absolute == filepath.Join(w.root, "state.json.report.sqlite-shm")

		if absolute == filepath.VolumeName(absolute)+string(filepath.Separator) || overlapStorage(absolute, w.root) && !strings.HasPrefix(absolute, w.root+string(filepath.Separator)+"crawls"+string(filepath.Separator)+r.ID+string(filepath.Separator)) && absolute != paths[0] && !legacyState {
			return nil, fmt.Errorf("storage path overlaps workspace: %s", absolute)
		}
		for id, other := range w.records {
			if id == r.ID {
				continue
			}
			owned := []string{filepath.Join(w.root, "crawls", id)}
			for _, old := range other.Runs {
				if old.Legacy && old.Expert != nil {
					owned = append(owned, pipelinePaths(*old.Expert)...)
					if p := old.Expert.Config.API.OperationsPath; p != "" {
						owned = append(owned, p)
					}
				}
			}
			for _, p := range owned {
				a, _ := filepath.Abs(p)
				if overlapStorage(absolute, a) {
					return nil, fmt.Errorf("storage is shared with another crawl")
				}
			}
		}
	}
	return paths, nil
}
func crawlLogEndpoints(r *CrawlRecord) ([][]string, error) {
	endpoints := [][]string{}
	seen := map[string]bool{}
	for _, run := range r.Runs {
		if run.Expert == nil {
			continue
		}
		for _, worker := range run.Expert.Workers {
			if worker.Type != "transformer" {
				continue
			}
			var p struct {
				Addresses []string `json:"search_addresses"`
				Enabled   []string `json:"enabled"`
			}
			if err := json.Unmarshal(worker.Parameters, &p); err != nil {
				return nil, err
			}
			if !run.LogsScoped {
				return nil, fmt.Errorf("unattributed Elasticsearch logs prevent log deletion; preserve_logs=true required")
			}
			for _, name := range p.Enabled {
				if name != "composite" && name != "metric" {
					return nil, fmt.Errorf("cannot delete logs for custom transformer %q", name)
				}
			}
			key := strings.Join(p.Addresses, "\x00")
			if !seen[key] {
				seen[key] = true
				endpoints = append(endpoints, p.Addresses)
			}
		}
	}
	return endpoints, nil
}
func (w *Workspace) acceptDeletion(r *CrawlRecord, slot *crawlSlot, revision uint64, id string, confirm bool, preserve *bool) (interface{}, error) {
	if !confirm {
		return nil, errs.NewRequestError("deletion confirmation required")
	}
	keep := preserve == nil || *preserve
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closing || w.ctx.Err() != nil {
		return nil, errs.NewRequestError("workspace is shutting down")
	}
	if w.records[r.ID] != r {
		return map[string]interface{}{"deleted": true}, nil
	}
	if id == "" {
		id = string(types.NewV4())
	}
	if op := r.Operations[id]; op != nil {
		if op.Action != "delete" || op.Version != revision || r.Deletion == nil || r.Deletion.PreserveLogs != keep {
			return nil, errs.NewRequestError("operation ID was already used for another request")
		}
		copy := *op
		return copy, nil
	}
	if activeWorkflow(r) {
		return nil, errs.NewRequestError("crawl operation in progress")
	}
	if revision != r.Revision {
		return nil, errs.NewRequestError("crawl revision conflict")
	}
	paths, err := w.deletionPaths(r)
	if err != nil {
		return nil, err
	}
	var endpoints [][]string
	if !keep {
		endpoints, err = crawlLogEndpoints(r)
		if err != nil {
			return nil, err
		}
	}
	if r.Operations == nil {
		r.Operations = map[string]*Operation{}
	}
	op := &Operation{ID: id, Action: "delete", Version: revision, State: "accepted", Updated: time.Now().Unix()}
	previous := r.Deletion
	r.Deletion = &CrawlDeletion{PreserveLogs: keep}
	r.Operations[id] = op
	if err := w.saveLocked(); err != nil {
		r.Deletion = previous
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
		if err == nil && slot.supervisor != nil {
			err = stopSupervisor(slot.supervisor)
			if err == nil {
				err = slot.supervisor.Close()
			}
			if err == nil {
				slot.setSupervisor(nil)
			}
		}
		if err == nil {
			for _, addresses := range endpoints {
				ctx, cancel := context.WithTimeout(w.ctx, 2*time.Minute)
				err = gateway.DeleteCrawlLogs(ctx, addresses, r.ID)
				cancel()
				if err != nil {
					break
				}
			}
		}
		if err == nil {
			for _, path := range paths {
				if err = os.RemoveAll(path); err != nil {
					break
				}
				report.Forget(path)
			}
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		if err == nil {
			delete(w.records, r.ID)
			delete(w.slots, r.ID)
			err = w.saveLocked()
			if err == nil {
				return
			}
			w.records[r.ID] = r
			w.slots[r.ID] = slot
		}
		op.State = "failed"
		op.Error = sanitizeText(err.Error(), configurationForSecrets(r.Definition))
		for _, run := range r.Runs {
			if run.Expert != nil {
				op.Error = sanitizeText(op.Error, *run.Expert)
			}
		}
		op.Updated = time.Now().Unix()
		w.saveLocked()
	}()
	copy := *op
	return copy, nil
}
