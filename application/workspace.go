package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/errs"
	"github.com/iakinsey/delver/util"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type CrawlRun struct {
	ConfigurationSnapshot json.RawMessage     `json:"configuration_snapshot,omitempty"`
	Desired               string              `json:"desired"`
	LogsScoped            bool                `json:"logs_scoped,omitempty"`
	Continued             int                 `json:"continued,omitempty"`
	Summary               *RuntimeSnapshot    `json:"summary,omitempty"`
	Legacy                bool                `json:"legacy,omitempty"`
	ID                    string              `json:"id"`
	Version               uint64              `json:"version"`
	Created               int64               `json:"created"`
	State                 string              `json:"state"`
	Elapsed               int64               `json:"elapsed_ns"`
	Definition            CrawlDefinition     `json:"definition"`
	Expert                *config.Application `json:"expert,omitempty"`
}
type CrawlRecord struct {
	Deletion     *CrawlDeletion        `json:"deletion,omitempty"`
	LegacySource string                `json:"legacy_source,omitempty"`
	CreationID   string                `json:"creation_id,omitempty"`
	CreationHash string                `json:"creation_hash,omitempty"`
	DraftID      string                `json:"draft_id,omitempty"`
	DraftHash    string                `json:"draft_hash,omitempty"`
	Operations   map[string]*Operation `json:"operations"`
	ID           string                `json:"id"`
	Revision     uint64                `json:"revision"`
	Archived     bool                  `json:"archived"`
	Definition   CrawlDefinition       `json:"definition"`
	Draft        *CrawlDefinition      `json:"draft,omitempty"`
	Expert       *config.Application   `json:"expert,omitempty"`
	ExpertDraft  *config.Application   `json:"expert_draft,omitempty"`
	Runs         []CrawlRun            `json:"runs"`
}
type crawlSlot struct {
	runtimeMu    sync.RWMutex
	initError    string
	inspect      chan struct{}
	mu           sync.Mutex
	supervisor   *Supervisor
	draining     bool
	stable       int
	clockMark    time.Time
	clockRunning bool
}
type Workspace struct {
	repeatedJobs       map[string]context.CancelFunc
	protectionPreviews map[string]protectionPreview
	protection         *protection.Policy
	atlasJobs          map[string]*atlasJob
	reportImports      map[string]bool
	settings           WorkspaceSettings
	broker             *flow.Broker
	dirty              bool
	persistenceError   string
	background         sync.WaitGroup
	closing            bool
	checks             sync.WaitGroup
	mu                 sync.Mutex
	ctx                context.Context
	cancel             context.CancelFunc
	options            Options
	root               string
	records            map[string]*CrawlRecord
	slots              map[string]*crawlSlot
	lock               *os.File
	gate               *util.OriginGate
	done               chan struct{}
}

func NewWorkspace(parent context.Context, options Options) (*Workspace, error) {
	app, err := DefaultApplication(options)
	if err != nil {
		return nil, err
	}
	root := filepath.Dir(app.Config.API.OperationsPath)
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(root, "workspace.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, errs.NewRequestError("workspace is already open")
	}
	ctx, cancel := context.WithCancel(parent)
	w := &Workspace{settings: WorkspaceSettings{Revision: 1, Execution: flow.DefaultExecutionLimits()}, ctx: ctx, cancel: cancel, options: options, root: root, records: map[string]*CrawlRecord{}, slots: map[string]*crawlSlot{}, lock: lock, gate: util.NewOriginGate(), done: make(chan struct{})}
	w.protection, err = protection.Open(filepath.Join(root, "protection.sqlite"))
	if err != nil {
		w.CloseWithoutMonitor()
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(root, "crawls.json"))
	if err == nil {
		if err = w.loadRegistry(b); err != nil {
			w.CloseWithoutMonitor()
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		w.CloseWithoutMonitor()
		return nil, err
	}
	w.broker = flow.NewBroker(w.settings.Execution)
	w.gate.SetLimits(w.settings.Execution.HostConcurrency, w.settings.Execution.HostDelay.Value())
	w.ctx = flow.WithBroker(w.ctx, w.broker)
	if w.records == nil {
		w.CloseWithoutMonitor()
		return nil, errs.NewRequestError("invalid workspace registry")
	}
	if options.initial != nil {
		found := false
		for _, record := range w.records {
			if record.Expert != nil && sameLegacyPipeline(*record.Expert, *options.initial) {
				found = true
			}
		}
		if !found {
			if e := w.importLegacy(*options.initial); e != nil {
				w.CloseWithoutMonitor()
				return nil, e
			}
		}
	} else if options.ImportPath != "" {
		a, e := Load(options.ImportPath)
		if e != nil {
			w.CloseWithoutMonitor()
			return nil, e
		}
		exists := false
		for _, r := range w.records {
			if source, _ := filepath.Abs(options.ImportPath); r.LegacySource != "" && r.LegacySource == source {
				exists = true
			}
			for _, run := range r.Runs {
				if run.Legacy && run.Expert != nil && sameLegacyPipeline(a, *run.Expert) {
					exists = true
				}
			}
		}
		if !exists {
			if e = w.importLegacy(a); e != nil {
				w.CloseWithoutMonitor()
				return nil, e
			}
		}
	} else if len(w.records) == 0 && os.IsNotExist(err) {
		if data, e := os.ReadFile(filepath.Join(root, "state.json")); e == nil {
			var old operationsState
			if e := json.Unmarshal(data, &old); e != nil {
				w.CloseWithoutMonitor()
				return nil, e
			}
			if len(old.Active.Workers) > 0 {
				if e := w.importLegacy(old.Active); e != nil {
					w.CloseWithoutMonitor()
					return nil, e
				}
			}
		}
	}

	for id, record := range w.records {
		if record == nil || record.ID != id || !validWorkspaceID(id) {
			w.CloseWithoutMonitor()
			return nil, fmt.Errorf("invalid stored crawl identity")
		}
		for _, run := range record.Runs {
			if !validWorkspaceID(run.ID) {
				w.CloseWithoutMonitor()
				return nil, fmt.Errorf("invalid stored run identity")
			}
		}
	}
	if e := w.importRunState(); e != nil {
		w.CloseWithoutMonitor()
		return nil, e
	}
	if w.dirty {
		if e := w.saveLocked(); e != nil {
			w.CloseWithoutMonitor()
			return nil, e
		}
	}
	interrupted := false
	for _, record := range w.records {
		for key, op := range record.Operations {
			if op == nil || (op.ID != key && op.RunID+":"+op.ID != key) {
				w.CloseWithoutMonitor()
				return nil, errs.NewRequestError("invalid persisted crawl operation")
			}
			if op.State != "accepted" && op.State != "running" && op.State != "completed" && op.State != "failed" {
				w.CloseWithoutMonitor()
				return nil, errs.NewRequestError("invalid persisted crawl operation state")
			}
			if op.State == "accepted" || op.State == "running" {
				op.State = "failed"
				op.Error = "Application restarted before the operation completed"
				op.Updated = time.Now().Unix()
				interrupted = true
			}
		}
	}
	if interrupted {
		if err := w.saveLocked(); err != nil {
			w.CloseWithoutMonitor()
			return nil, err
		}
	}
	for id, r := range w.records {
		if r == nil || r.ID != id || !validWorkspaceID(id) {
			w.CloseWithoutMonitor()
			return nil, errs.NewRequestError("invalid stored crawl identity")
		}
		slot := newCrawlSlot()
		w.slots[id] = slot
		if len(r.Runs) > 0 && r.Deletion == nil {
			run := r.Runs[len(r.Runs)-1]
			if !validWorkspaceID(run.ID) {
				w.CloseWithoutMonitor()
				return nil, errs.NewRequestError("invalid stored run identity")
			}
			s, e := w.openSupervisor(r, run)
			if e != nil {
				slot.initError = sanitizeText(e.Error(), configurationForSecrets(r.Definition))
				continue
			}
			slot.supervisor = s
			if !r.Archived && run.State != "completed" {
				s.mu.Lock()
				if s.state.Desired != "stopped" {
					if e := s.start(); e != nil {
						s.lastError = e.Error()
					}
				}
				s.mu.Unlock()
			}
		}
	}
	w.background.Add(1)
	go w.protectionLoop()
	go w.monitor()
	return w, nil
}
func validWorkspaceID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r != '-' && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
func (w *Workspace) runRoot(id, run string) string {
	return filepath.Join(w.root, "crawls", id, "runs", run)
}
func (w *Workspace) saveLocked() (err error) {
	defer func() {
		if err != nil {
			w.persistenceError = err.Error()
			w.dirty = true
		} else {
			w.persistenceError = ""
			w.dirty = false
		}
	}()
	b, err := json.MarshalIndent(workspaceRegistry{Version: 2, Settings: w.settings, Crawls: w.records}, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(w.root, "crawls.json")
	f, err := os.OpenFile(path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(path+".tmp", path); err != nil {
		return err
	}
	d, err := os.Open(w.root)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (w *Workspace) openSupervisor(r *CrawlRecord, run CrawlRun) (*Supervisor, error) {
	root := w.runRoot(r.ID, run.ID)
	a, err := prepareRunPipeline(root, run)
	if err != nil {
		return nil, err
	}
	if !run.Legacy {
		a.Config.API.OperationsPath = filepath.Join(root, "state.json")
	}
	a.Config.API.DashDBPath = filepath.Join(root, "dash.db")
	a.Config.API.Enabled = false
	a.Config.Streamer.Enabled = false
	a.Config.HTTPClient.Protection = w.protection
	s, err := newSupervisor(w.ctx, a)
	if err != nil {
		return nil, err
	}
	s.startupTimeout = w.options.StartupTimeout
	if s.startupTimeout == 0 {
		s.startupTimeout = 5 * time.Minute
	}
	s.crawlID, s.runID = r.ID, run.ID
	s.state.Version = run.Version
	s.state.Desired = run.Desired
	if s.state.Desired == "" {
		s.state.Desired = "stopped"
	}
	boundRunID := run.ID
	s.onDesired = func(desired string) error {
		w.mu.Lock()
		defer w.mu.Unlock()
		if len(r.Runs) == 0 || r.Runs[len(r.Runs)-1].ID != boundRunID {
			return fmt.Errorf("run identity changed")
		}
		previous := r.Runs[len(r.Runs)-1].Desired
		r.Runs[len(r.Runs)-1].Desired = desired
		if err := w.saveLocked(); err != nil {
			r.Runs[len(r.Runs)-1].Desired = previous
			return err
		}
		return nil
	}
	s.originGate = w.gate
	s.log.policy = w.protection
	s.onFinalSnapshot = func(snapshot RuntimeSnapshot) {
		w.saveRunSummary(r.ID, boundRunID, sanitizeRuntimeSnapshot(snapshot, s.state.Active))
	}
	s.onStatus = func(state string, now time.Time) {
		if state == "stopping" && s.control != nil {
			w.saveRunSummary(r.ID, boundRunID, sanitizeRuntimeSnapshot(s.control.snapshot(), s.state.Active))
		}
		w.onRunStatus(r.ID, boundRunID, state, now)
	}
	return s, nil
}
func relocatePipeline(a config.Application, root string) config.Application {
	b, _ := json.Marshal(a)
	var clone config.Application
	json.Unmarshal(b, &clone)
	relocate := func(raw json.RawMessage, prefix string) json.RawMessage {
		var value interface{}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		decoder.Decode(&value)
		var visit func(interface{})
		visit = func(v interface{}) {
			switch obj := v.(type) {
			case map[string]interface{}:
				for k, child := range obj {
					if text, ok := child.(string); ok && (k == "path" || k == "base_path" || k == "dlq_path") {
						relative, err := filepath.Rel(root, text)
						if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
							obj[k] = filepath.Join(root, fmt.Sprintf("%x", sha256.Sum256([]byte(prefix))), k, filepath.Base(text))
						}
					} else {
						visit(child)
					}
				}
			case []interface{}:
				for _, child := range obj {
					visit(child)
				}
			}
		}
		visit(value)
		out, _ := json.Marshal(value)
		return out
	}
	for i, r := range clone.Resources {
		clone.Resources[i].Parameters = relocate(r.Parameters, "resource-"+r.Name)
	}
	for i, v := range clone.Workers {
		clone.Workers[i].Parameters = relocate(v.Parameters, "worker-"+v.Name)
	}
	clone.Config.API.Enabled = false
	clone.Config.Streamer.Enabled = false
	return clone
}
func (w *Workspace) CloseWithoutMonitor() {
	defer func() {
		if w.protection != nil {
			w.protection.Close()
		}
	}()
	w.cancel()
	for _, slot := range w.slots {
		if slot.supervisor != nil {
			slot.supervisor.Close()
		}
	}
	if w.lock != nil {
		syscall.Flock(int(w.lock.Fd()), syscall.LOCK_UN)
		w.lock.Close()
		w.lock = nil
	}
}
func (w *Workspace) Close() error {
	defer func() {
		if w.protection != nil {
			w.protection.Close()
		}
	}()
	w.mu.Lock()
	w.closing = true
	w.mu.Unlock()
	w.cancel()
	w.background.Wait()
	<-w.done
	w.mu.Lock()
	slots := make([]*crawlSlot, 0, len(w.slots))
	for _, s := range w.slots {
		slots = append(slots, s)
	}
	w.mu.Unlock()
	var first error
	for _, slot := range slots {
		slot.mu.Lock()
		if slot.supervisor != nil {
			if err := slot.supervisor.Close(); err != nil && first == nil {
				first = err
			}
		}
		slot.mu.Unlock()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.dirty {
		if err := w.saveLocked(); err != nil && first == nil {
			first = err
		}
	}
	if w.lock != nil {
		syscall.Flock(int(w.lock.Fd()), syscall.LOCK_UN)
		w.lock.Close()
		w.lock = nil
	}
	return first
}
func publicValue(v interface{}) interface{} {
	b, _ := json.Marshal(v)
	var value interface{}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	decoder.Decode(&value)
	return redactValue(value)
}
func (w *Workspace) snapshot(id string) (interface{}, error) {
	w.mu.Lock()
	r := w.records[id]
	slot := w.slots[id]
	if r == nil {
		w.mu.Unlock()
		return nil, errs.NewRequestError("crawl not found")
	}
	value := publicRecord(r).(map[string]interface{})
	if w.persistenceError != "" {
		value["persistence_error"] = w.persistenceError
	}
	w.mu.Unlock()
	slot.runtimeMu.RLock()
	supervisor, initError := slot.supervisor, slot.initError
	slot.runtimeMu.RUnlock()
	if initError != "" {
		value["runtime"] = map[string]string{"state": "failed", "error": initError}
	}
	if supervisor != nil {
		status, err := supervisor.Handle(w.ctx, "status", nil)
		if err != nil {
			return nil, err
		}
		value["runtime"] = status
	}
	return value, nil
}
func (w *Workspace) HandleCrawls(ctx context.Context, action string, raw json.RawMessage) (interface{}, error) {
	if w.ctx.Err() != nil {
		return nil, errs.NewRequestError("workspace is shutting down")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var req struct {
		CrawlID           string          `json:"crawl_id"`
		Confirm           bool            `json:"confirm"`
		PreserveLogs      *bool           `json:"preserve_logs"`
		Revision          uint64          `json:"revision"`
		Definition        json.RawMessage `json:"definition"`
		Configuration     json.RawMessage `json:"configuration"`
		ConfigurationJSON *string         `json:"configuration_json"`
		Action            string          `json:"action"`
		ID                string          `json:"id"`
		Queue             string          `json:"queue"`
		Record            string          `json:"record"`
		Offset            int             `json:"offset"`
		RunID             string          `json:"run_id"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
	}
	if req.ConfigurationJSON != nil {
		if len(req.Configuration) > 0 {
			return nil, errs.NewRequestError("provide configuration or configuration_json, not both")
		}
		req.Configuration = json.RawMessage(*req.ConfigurationJSON)
	}
	if action == "patch_fetch" {
		return patchFetch(raw)
	}
	if action == "patch_repeated_pages" {
		return patchRepeatedPages(raw)
	}
	if action == "patch_queue_cache" {
		return patchQueueCache(raw)
	}
	if action == "preview" {
		return previewDefinition(raw)
	}
	if action == "defaults" {
		return publicValue(DefaultCrawlDefinition()), nil
	}
	if action == "list" {
		w.mu.Lock()
		ids := []string{}
		for id := range w.records {
			ids = append(ids, id)
		}
		w.mu.Unlock()
		sort.Strings(ids)
		out := []interface{}{}
		for _, id := range ids {
			v, err := w.snapshot(id)
			if err != nil {
				w.mu.Lock()
				gone := w.records[id] == nil
				w.mu.Unlock()
				if gone {
					continue
				}
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	}
	if action == "create" || action == "validate" {
		d, err := decodeDefinition(req.Definition)
		if err != nil {
			return nil, err
		}
		a, err := w.compileCrawl(filepath.Join(w.root, "preview"), "preview", d)
		if err != nil {
			return nil, err
		}
		if action == "validate" {
			return map[string]interface{}{"valid": true, "configuration": redactConfiguration(operationsState{Active: a})}, nil
		}
		digestBytes, _ := json.Marshal(d)
		digest := fmt.Sprintf("%x", sha256.Sum256(digestBytes))
		id := string(types.NewV4())
		w.mu.Lock()
		if req.ID != "" {
			for _, existing := range w.records {
				if existing.CreationID == req.ID {
					w.mu.Unlock()
					if existing.CreationHash != digest {
						return nil, errs.NewRequestError("creation request ID already belongs to another configuration")
					}
					return w.snapshot(existing.ID)
				}
			}
		}

		w.records[id] = &CrawlRecord{ID: id, Revision: 1, Definition: d, Runs: []CrawlRun{}, CreationID: req.ID, CreationHash: digest}
		w.slots[id] = newCrawlSlot()
		err = w.saveProtectedRecordLocked(w.records[id])
		if err != nil {
			delete(w.records, id)
			delete(w.slots, id)
		}
		w.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return w.snapshot(id)
	}
	if action == "delete" && !validWorkspaceID(req.CrawlID) {
		return nil, errs.NewRequestError("crawl ID required")
	}
	w.mu.Lock()
	r := w.records[req.CrawlID]
	slot := w.slots[req.CrawlID]
	if req.CrawlID == "" && len(w.records) == 1 {
		for _, record := range w.records {
			r = record
			slot = w.slots[record.ID]
		}
	}
	w.mu.Unlock()
	if r == nil {
		if action == "delete" && req.Confirm && validWorkspaceID(req.CrawlID) {
			return map[string]interface{}{"deleted": true}, nil
		}
		return nil, errs.NewRequestError("crawl ID required")
	}
	if strings.HasPrefix(action, "repeated_") {
		return w.handleRepeated(ctx, r, slot, action, raw)
	}
	if action == "trap_decisions" || action == "trap_restore" {
		return w.handleTraps(ctx, r, slot, action, raw)
	}
	if action == "report" || action == "hosts" || action == "host_graph" || strings.HasPrefix(action, "atlas_") {
		return w.handleReport(ctx, r, slot, action, raw)
	}
	if action == "delete" {
		return w.acceptDeletion(r, slot, req.Revision, req.ID, req.Confirm, req.PreserveLogs)
	}
	if action == "results" {
		return w.recentResults(r, req.RunID)
	}
	if action == "pipeline" {
		w.mu.Lock()
		defer w.mu.Unlock()
		if r.ExpertDraft != nil {
			return pipelinePreview(r.ExpertDraft), nil
		}
		if r.Expert != nil {
			return pipelinePreview(r.Expert), nil
		}
		d := r.Definition
		if r.Draft != nil {
			d = *r.Draft
		}
		root, runID := filepath.Join(w.root, "preview"), "preview"
		if len(r.Runs) > 0 {
			runID = r.Runs[len(r.Runs)-1].ID
			root = w.runRoot(r.ID, runID)
		}
		a, err := w.compileCrawl(root, runID, d)
		if err != nil {
			return nil, err
		}
		return pipelinePreview(a), nil
	}
	if action == "detail" {
		return w.snapshot(r.ID)
	}
	if action == "activate" || action == "start_run" || action == "continue_run" {
		return w.acceptWorkflow(r, slot, action, req.Revision, req.ID)
	}
	if action == "operations" {
		return w.crawlOperations(ctx, r, slot)
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	w.mu.Lock()
	unavailable := w.records[r.ID] != r || r.Deletion != nil
	w.mu.Unlock()
	if unavailable {
		return nil, errs.NewRequestError("crawl deletion is in progress; retry deletion if it failed")
	}
	if action == "draft" {
		w.mu.Lock()
		defer w.mu.Unlock()
		var requestValue interface{}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&requestValue); err != nil {
			return nil, err
		}
		canonical, err := json.Marshal(requestValue)
		if err != nil {
			return nil, err
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(canonical))
		if req.ID != "" && req.ID == r.DraftID {
			if digest != r.DraftHash {
				return nil, errs.NewRequestError("draft ID was already used for another request")
			}
			saved := *r
			saved.Revision = req.Revision + 1
			return publicRecord(&saved), nil
		}
		if activeWorkflow(r) {
			return nil, errs.NewRequestError("crawl operation in progress")
		}
		if req.Revision != r.Revision {
			return nil, errs.NewRequestError("crawl revision conflict")
		}
		before := *r
		if req.ConfigurationJSON != nil || len(req.Configuration) > 0 {
			base := config.Application{}
			d := r.Definition
			if r.Draft != nil {
				d = *r.Draft
			}
			if r.Expert == nil && r.ExpertDraft == nil {
				compiled, err := w.compileCrawl(filepath.Join(w.root, "preview"), "preview", d)
				if err != nil {
					return nil, err
				}
				base = compiled
			}
			if r.Expert != nil {
				base = *r.Expert
			}
			if r.ExpertDraft != nil {
				base = *r.ExpertDraft
			}
			data, err := restoreConfiguration(req.Configuration, base)
			if err != nil {
				return nil, err
			}
			a, err := DecodeBytes(data)
			if err != nil {
				return nil, err
			}
			a.Config.HTTPClient.Protection = w.protection
			if err := Validate(relocatePipeline(a, filepath.Join(w.root, "preview"))); err != nil {
				return nil, err
			}
			if len(req.Definition) > 0 {
				var metadata struct {
					Name        *string      `json:"name"`
					Termination *Termination `json:"termination"`
				}
				if err := json.Unmarshal(req.Definition, &metadata); err != nil {
					return nil, err
				}
				if metadata.Name != nil {
					if strings.TrimSpace(*metadata.Name) == "" {
						return nil, errs.NewRequestError("name required")
					}
					d.Name = *metadata.Name
				}
				if metadata.Termination != nil {
					d.Termination = *metadata.Termination
				}
			}
			if _, err := expertRuntimeControls(relocatePipeline(a, filepath.Join(w.root, "preview")), d.Termination); err != nil {
				return nil, err
			}
			r.ExpertDraft = &a
			r.Draft = &d
		} else {
			previous := r.Definition
			if r.Draft != nil {
				previous = *r.Draft
			}
			var value interface{}
			if err := json.Unmarshal(req.Definition, &value); err != nil {
				return nil, err
			}
			b, _ := json.Marshal(previous)
			var old interface{}
			json.Unmarshal(b, &old)
			data, _ := json.Marshal(restoreValue(value, old))
			d, err := decodeDefinition(data)
			if err != nil {
				return nil, err
			}
			if _, err := w.compileCrawl(filepath.Join(w.root, "preview"), "preview", d); err != nil {
				return nil, err
			}
			if r.Expert != nil || r.ExpertDraft != nil {
				return nil, errs.NewRequestError("custom pipelines must be edited in expert mode")
			}
			r.Draft = &d
		}
		r.DraftID, r.DraftHash = req.ID, digest
		r.Revision++
		if err := w.saveProtectedRecordLocked(r); err != nil {
			*r = before
			return nil, err
		}
		return publicRecord(r), nil
	}
	if action == "duplicate" {
		w.mu.Lock()
		defer w.mu.Unlock()
		b, _ := json.Marshal(r)
		var copy CrawlRecord
		json.Unmarshal(b, &copy)
		copy.ID = string(types.NewV4())
		copy.CreationID = ""
		copy.CreationHash = ""
		copy.DraftID, copy.DraftHash = "", ""
		copy.Revision = 1
		copy.Archived = false
		copy.Definition.Name += " copy"
		copy.Runs = []CrawlRun{}
		w.records[copy.ID] = &copy
		w.slots[copy.ID] = newCrawlSlot()
		if err := w.saveLocked(); err != nil {
			delete(w.records, copy.ID)
			delete(w.slots, copy.ID)
			return nil, err
		}
		return publicRecord(&copy), nil
	}
	if action == "archive" || action == "restore" {
		if action == "archive" && slot.supervisor != nil {
			if err := stopSupervisor(slot.supervisor); err != nil {
				return nil, err
			}
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		previousArchived, previousRevision := r.Archived, r.Revision
		r.Archived = action == "archive"
		r.Revision++
		if err := w.saveLocked(); err != nil {
			r.Archived, r.Revision = previousArchived, previousRevision
			return nil, err
		}
		return publicRecord(r), nil
	}

	if action == "command" && (req.Action == "start" || req.Action == "resume") {
		return w.acceptWorkflow(r, slot, req.Action, r.Revision, req.ID, Operation{})
	}
	if slot.supervisor == nil {
		return nil, errs.NewRequestError("run required")
	}
	switch action {
	case "command":
		switch req.Action {
		case "start", "resume", "stop", "pause", "replay", "recrawl":
		default:
			return nil, errs.NewRequestError("unknown execution action")
		}
		return w.acceptWorkflow(r, slot, req.Action, r.Revision, req.ID, Operation{Queue: req.Queue, Record: req.Record})
	case "status", "failed", "log":
		return slot.supervisor.Handle(ctx, action, raw)
	case "operations":
		return w.crawlOperations(ctx, r, slot)
	case "pipeline":
		slot.supervisor.mu.Lock()
		defer slot.supervisor.mu.Unlock()
		return pipelinePreview(slot.supervisor.state.Active), nil
	}
	return nil, errs.NewRequestError("unknown crawl action")
}
func stopSupervisor(s *Supervisor) error {
	s.transition.Lock()
	defer s.transition.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.setDesired("stopped"); err != nil {
		return err
	}
	return s.stop()
}

func newCrawlSlot() *crawlSlot { return &crawlSlot{inspect: make(chan struct{}, 1)} }

func configurationForSecrets(d CrawlDefinition) config.Application {
	c := config.LoadConfig()
	c.HTTPClient.HTTPProxyUrl = d.Proxy.URL
	return config.Application{Config: c}
}

func (w *Workspace) applyRun(r *CrawlRecord, slot *crawlSlot, action string, revision uint64) (interface{}, error) {
	if action == "continue_run" {
		return w.continueLatest(r, slot, revision)
	}
	w.mu.Lock()
	if r.Archived {
		w.mu.Unlock()
		return nil, errs.NewRequestError("crawl archived")
	}
	if revision != r.Revision {
		w.mu.Unlock()
		return nil, errs.NewRequestError("crawl revision conflict")
	}
	d := r.Definition
	expert := r.Expert
	if r.Draft != nil || r.ExpertDraft != nil {
		if r.Draft != nil {
			d = *r.Draft
		}
		if r.ExpertDraft != nil {
			expert = r.ExpertDraft
		}
	}
	execution := w.settings.Execution
	run := CrawlRun{Desired: "running", LogsScoped: true, ID: string(types.NewV4()), Created: time.Now().Unix()}
	if len(r.Runs) > 0 {
		run = r.Runs[len(r.Runs)-1]
	}
	run.Definition, run.Expert = d, expert
	if r.Draft != nil || r.ExpertDraft != nil {
		run.Continued = 0
	}
	var previous *config.Application
	if len(r.Runs) > 0 {
		app, err := prepareRunPipeline(w.runRoot(r.ID, run.ID), r.Runs[len(r.Runs)-1])
		if err != nil {
			w.mu.Unlock()
			return nil, err
		}
		previous = &app
	}
	if previous != nil && r.Runs[len(r.Runs)-1].Expert == nil && expert != nil {
		bound, err := bindExistingStorage(*expert, *previous)
		if err != nil {
			w.mu.Unlock()
			return nil, err
		}
		expert = &bound
		run.Expert = expert
	}
	w.mu.Unlock()
	// Validate against the existing run's storage before interrupting execution.
	// Starting or applying configuration never allocates a replacement frontier.
	prepared, err := prepareRunPipeline(w.runRoot(r.ID, run.ID), run)
	if err != nil {
		return nil, err
	}
	prepared.Config.HTTPClient.Protection = w.protection
	if err := Validate(prepared); err != nil {
		return nil, err
	}
	if previous != nil {
		if err := validateExistingStorage(*previous, prepared); err != nil {
			return nil, err
		}
	}
	if slot.supervisor != nil {
		if err := stopSupervisor(slot.supervisor); err != nil {
			return nil, err
		}
		if err := slot.supervisor.Close(); err != nil {
			return nil, err
		}
		slot.setSupervisor(nil)
	}
	if previous != nil {
		if err := resetChangedSources(*previous, prepared); err != nil {
			return nil, err
		}
	}
	// Shutdown checkpoints elapsed time and final metrics. Read those after all
	// workers finish, rather than replacing them with the pre-shutdown snapshot.
	w.mu.Lock()
	if len(r.Runs) > 0 {
		latest := r.Runs[len(r.Runs)-1]
		run.Elapsed, run.Summary = latest.Elapsed, latest.Summary
	}
	run.Version, run.State, run.Desired = r.Revision+1, "starting", "running"
	if len(run.ConfigurationSnapshot) == 0 {
		run.ConfigurationSnapshot = configurationSnapshot(d, expert, execution)
	}
	w.mu.Unlock()
	s, err := w.openSupervisor(r, run)
	if err != nil {
		return nil, err
	}
	slot.setSupervisor(s)
	slot.draining = false
	slot.stable = 0

	w.mu.Lock()
	before := *r
	before.Runs = append([]CrawlRun(nil), r.Runs...)
	r.Definition = d
	r.Expert = expert
	r.Draft = nil
	r.ExpertDraft = nil
	r.Revision++
	slot.clockRunning = false
	slot.clockMark = time.Now()
	if len(r.Runs) == 0 {
		r.Runs = append(r.Runs, run)
	} else {
		r.Runs[len(r.Runs)-1] = run
	}
	err = w.saveLocked()
	w.mu.Unlock()
	if err != nil {
		stopSupervisor(s)
		s.Close()
		slot.setSupervisor(nil)
		w.mu.Lock()
		*r = before
		w.mu.Unlock()
		return nil, err
	}
	s.mu.Lock()
	err = s.setDesired("running")
	if err == nil {
		err = s.start()
	}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return map[string]string{"state": "running", "run_id": run.ID}, nil

}

func publicRecord(r *CrawlRecord) interface{} {
	v := publicValue(r).(map[string]interface{})
	delete(v, "creation_hash")
	delete(v, "draft_hash")
	delete(v, "legacy_source")
	return v
}

func pipelinePreview(v interface{}) map[string]interface{} {
	visible := publicValue(v)
	text, _ := json.MarshalIndent(visible, "", "  ")
	return map[string]interface{}{"configuration": visible, "configuration_json": string(text)}
}
