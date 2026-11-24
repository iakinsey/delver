package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/types/errs"
	"github.com/iakinsey/delver/util"
)

type Operation struct {
	Progress        map[string]int64          `json:"progress,omitempty"`
	CancelRequested bool                      `json:"cancel_requested,omitempty"`
	Input           json.RawMessage           `json:"input,omitempty"`
	Analysis        *RepeatedAnalysisProgress `json:"analysis,omitempty"`
	RunID           string                    `json:"run_id,omitempty"`
	Version         uint64                    `json:"configuration_version,omitempty"`
	Queue           string                    `json:"queue,omitempty"`
	Record          string                    `json:"record,omitempty"`
	ID              string                    `json:"id"`
	Action          string                    `json:"action"`
	State           string                    `json:"state"`
	Error           string                    `json:"error,omitempty"`
	Updated         int64                     `json:"updated"`
}
type operationsState struct {
	Desired    string                `json:"desired"`
	Active     config.Application    `json:"active"`
	Draft      *config.Application   `json:"draft,omitempty"`
	Previous   *config.Application   `json:"previous,omitempty"`
	Version    uint64                `json:"version"`
	Operations map[string]*Operation `json:"operations"`
}

// operationsState is read only by the one-time legacy importer.
type executionState struct {
	Desired string
	Active  config.Application
	Version uint64
}
type runtimeResult struct{ err error }

type Supervisor struct {
	startupTimeout  time.Duration
	onDesired       func(string) error
	crawlID, runID  string
	onStatus        func(string, time.Time)
	onFinalSnapshot func(RuntimeSnapshot)
	originGate      config.HTTPGate
	log             crawlLog
	transition      sync.Mutex
	result          *runtimeResult
	lockFile        *os.File
	closing         bool
	mu              sync.Mutex
	state           executionState
	path            string
	status          string
	lastError       string
	control         *runtimeControl
	cancel          context.CancelFunc
	done            chan struct{}
	ctx             context.Context
}

func newSupervisor(ctx context.Context, app config.Application) (*Supervisor, error) {
	if err := Validate(app); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(app)
	if err != nil {
		return nil, err
	}
	var owned config.Application
	if err := json.Unmarshal(encoded, &owned); err != nil {
		return nil, err
	}
	owned.Config.API.StaticFiles = app.Config.API.StaticFiles
	owned.Config.HTTPClient.Protection = app.Config.HTTPClient.Protection
	owned.Config.HTTPClient.LocalGate = app.Config.HTTPClient.LocalGate
	owned.Config.HTTPClient.GlobalGate = app.Config.HTTPClient.GlobalGate
	app = owned
	path := app.Config.API.OperationsPath
	if path == "" {
		path = app.Config.API.DashDBPath + ".operations.json"
	}
	s := &Supervisor{startupTimeout: app.Config.ShutdownTimeout, path: path, ctx: ctx, status: "stopped", state: executionState{Desired: "stopped", Active: app, Version: 1}}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, fmt.Errorf("operations state already owned: %w", err)
	}
	s.lockFile = lock
	success := false
	defer func() {
		if !success {
			syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
			lock.Close()
		}
	}()

	success = true
	return s, nil
}
func (s *Supervisor) start() error {
	if s.done != nil {
		return fmt.Errorf("runtime already exists")
	}
	s.setStatus("starting")
	ctx, cancel := context.WithCancel(s.ctx)
	s.cancel = cancel
	s.control = &runtimeControl{log: &s.log, onFinalSnapshot: s.onFinalSnapshot, initialPaused: s.state.Desired == "paused", resourcesDone: make(chan struct{})}
	s.done = make(chan struct{})
	s.result = &runtimeResult{}
	control, done, app, result := s.control, s.done, s.state.Active, s.result
	app.Config.RuntimeCrawlID, app.Config.RuntimeRunID = s.crawlID, s.runID
	app.Config.HTTPClient.ProtectionCrawl, app.Config.HTTPClient.ProtectionRun = s.crawlID, s.runID
	if s.originGate != nil {
		app.Config.HTTPClient.GlobalGate = s.originGate
		app.Config.HTTPClient.LocalGate = util.NewOriginGate()
	}
	go func() { result.err = Run(context.WithValue(ctx, runtimeKey{}, control), app); close(done) }()
	deadline := time.NewTimer(s.startupTimeout)
	defer deadline.Stop()
	for {
		control.mu.Lock()
		ready := control.prepared != nil
		control.mu.Unlock()
		if ready {
			if control.initialPaused {
				s.setStatus("paused")
			} else {
				s.setStatus("running")
			}
			return nil
		}
		s.mu.Unlock()
		select {
		case <-done:
			s.mu.Lock()
			err := result.err
			select {
			case <-control.resourcesDone:
				s.done = nil
				s.control = nil
			default:
			}
			s.setStatus("failed")
			if err == nil {
				err = fmt.Errorf("runtime stopped during startup")
			}
			return err
		case <-deadline.C:
			s.mu.Lock()
			cancel()
			s.setStatus("failed")
			return fmt.Errorf("startup timeout")
		case <-time.After(10 * time.Millisecond):
			s.mu.Lock()
		}
	}
}
func (s *Supervisor) stop() error {
	if s.done == nil {
		s.setStatus("stopped")
		return nil
	}
	s.setStatus("stopping")
	s.cancel()
	done, control, result := s.done, s.control, s.result
	timeout := s.state.Active.Config.ShutdownTimeout
	s.mu.Unlock()
	defer s.mu.Lock()
	select {
	case <-done:
		select {
		case <-control.resourcesDone:
			s.mu.Lock()
			s.done = nil
			s.control = nil
			s.setStatus("stopped")
			s.mu.Unlock()
			return result.err
		case <-time.After(timeout):
			s.mu.Lock()
			s.setStatus("failed")
			s.mu.Unlock()
			return fmt.Errorf("runtime has not released its resources: %v", result.err)
		}
	case <-time.After(timeout):
		s.mu.Lock()
		s.setStatus("failed")
		s.mu.Unlock()
		return fmt.Errorf("runtime has not released its resources")
	}

}
func (s *Supervisor) Handle(ctx context.Context, action string, raw json.RawMessage) (interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch action {
	case "status":
		if s.done != nil && s.status != "stopping" {
			select {
			case <-s.done:
				s.setStatus("failed")
				if s.result.err != nil {
					s.lastError = s.result.err.Error()
				}
			default:
			}
		}

		result := map[string]interface{}{"state": s.status, "desired": s.state.Desired, "version": s.state.Version, "error": s.safeError(s.lastError), "sources": sourceSummaries(s.state.Active), "crawl_ids": crawlIdentities(s.state.Active)}
		if s.control != nil {
			snapshot := s.control.snapshot()
			for name, activity := range snapshot.Workers {
				activity.LastError = s.safeError(activity.LastError)
				snapshot.Workers[name] = activity
			}
			for name, err := range snapshot.Health {
				snapshot.Health[name] = s.safeError(err)
			}
			result["runtime"] = snapshot
		}
		return result, nil
	case "log":
		entries := s.log.snapshot()
		for i := range entries {
			entries[i].URI = s.safeError(entries[i].URI)
			entries[i].Error = s.safeError(entries[i].Error)
		}
		return entries, nil
	case "failed":
		var input struct {
			Queue  string `json:"queue"`
			Offset int    `json:"offset"`
		}
		if err := json.Unmarshal(raw, &input); err != nil {
			return nil, err
		}
		return s.failed(input.Queue, input.Offset)
	}
	return nil, errs.NewRequestError("unknown operation")
}
func DecodeBytes(data []byte) (config.Application, error) { return Decode(bytes.NewReader(data)) }

// Execute performs one serialized execution action. Workspace owns its journal.
func (s *Supervisor) Execute(ctx context.Context, action, queueName, record string) error {
	s.transition.Lock()
	defer s.transition.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return fmt.Errorf("service is shutting down")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	switch action {
	case "start", "resume":
		err = s.setDesired("running")
		if err == nil {
			if s.done == nil {
				err = s.start()
			} else {
				err = s.control.pause(ctx, false)
				if err == nil {
					s.setStatus("running")
				}
			}
		}
	case "stop":
		err = s.setDesired("stopped")
		if err == nil {
			err = s.stop()
		}
	case "pause":
		if s.control == nil {
			return fmt.Errorf("runtime is stopped")
		}
		err = s.setDesired("paused")
		if err == nil {
			s.setStatus("pausing")
			control := s.control
			deadline, cancel := context.WithTimeout(ctx, s.state.Active.Config.ShutdownTimeout)
			s.mu.Unlock()
			err = control.pause(deadline, true)
			s.mu.Lock()
			cancel()
			if err == nil {
				s.setStatus("paused")
			}
		}
	case "replay", "recrawl":
		err = s.recoverRecord(queueName, record, action == "recrawl")
	default:
		return fmt.Errorf("unknown execution action")
	}
	if err != nil {
		s.lastError = err.Error()
	} else {
		s.lastError = ""
	}
	return err
}
func (s *Supervisor) controlPause(ctx context.Context) error {
	if s.control == nil {
		return fmt.Errorf("runtime is stopped")
	}
	return s.control.pause(ctx, true)
}

// Serve imports a pipeline into the workspace server rather than creating a second control surface.
func Serve(ctx context.Context, app config.Application) error {
	if !app.Config.API.Enabled {
		return Run(ctx, app)
	}
	statePath := app.Config.API.OperationsPath
	if statePath == "" {
		statePath = app.Config.API.DashDBPath + ".operations.json"
	}
	return ServeWorkspace(ctx, Options{Address: app.Config.API.Address, DataDirectory: filepath.Dir(statePath), initial: &app})
}
func redactConfiguration(state operationsState) interface{} {
	app := state.Active
	if state.Draft != nil {
		app = *state.Draft
	}
	app.Config.API = state.Active.Config.API
	app.Config.Streamer = state.Active.Config.Streamer
	return map[string]interface{}{"configuration": redactValue(configValue(app)), "version": state.Version}
}

func NewSupervisor(ctx context.Context, app config.Application) (*Supervisor, error) {
	return newSupervisor(ctx, app)
}
func (s *Supervisor) Close() error {
	s.mu.Lock()
	s.closing = true
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	s.transition.Lock()
	s.mu.Lock()
	err := s.stop()
	s.mu.Unlock()
	s.transition.Unlock()
	s.mu.Lock()
	if s.lockFile != nil && s.done == nil {
		syscall.Flock(int(s.lockFile.Fd()), syscall.LOCK_UN)
		s.lockFile.Close()
		s.lockFile = nil
	}
	s.mu.Unlock()
	return err
}

func (s *Supervisor) setDesired(desired string) error {
	old := s.state.Desired
	s.state.Desired = desired
	if s.onDesired != nil {
		if err := s.onDesired(desired); err != nil {
			s.state.Desired = old
			return err
		}
	}
	return nil
}

func (s *Supervisor) setStatus(status string) {
	s.status = status
	if s.onStatus != nil {
		s.onStatus(status, time.Now())
	}
}
