package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"syscall"
	"time"

	"github.com/iakinsey/delver/api"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/gateway"
	"github.com/iakinsey/delver/types/errs"
	"github.com/iakinsey/delver/util"
)

type Operation struct {
	Version uint64 `json:"configuration_version,omitempty"`
	Queue   string `json:"queue,omitempty"`
	Record  string `json:"record,omitempty"`
	ID      string `json:"id"`
	Action  string `json:"action"`
	State   string `json:"state"`
	Error   string `json:"error,omitempty"`
	Updated int64  `json:"updated"`
}
type operationsState struct {
	Desired    string                `json:"desired"`
	Active     config.Application    `json:"active"`
	Draft      *config.Application   `json:"draft,omitempty"`
	Previous   *config.Application   `json:"previous,omitempty"`
	Version    uint64                `json:"version"`
	Operations map[string]*Operation `json:"operations"`
}
type runtimeResult struct{ err error }

type Supervisor struct {
	onStatus   func(string, time.Time)
	originGate config.HTTPGate
	log        crawlLog
	transition sync.Mutex
	result     *runtimeResult
	lockFile   *os.File
	closing    bool
	commands   sync.WaitGroup
	mu         sync.Mutex
	state      operationsState
	path       string
	status     string
	lastError  string
	control    *runtimeControl
	cancel     context.CancelFunc
	done       chan struct{}
	ctx        context.Context
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
	owned.Config.HTTPClient.LocalGate = app.Config.HTTPClient.LocalGate
	owned.Config.HTTPClient.GlobalGate = app.Config.HTTPClient.GlobalGate
	app = owned
	path := app.Config.API.OperationsPath
	if path == "" {
		path = app.Config.API.DashDBPath + ".operations.json"
	}
	s := &Supervisor{path: path, ctx: ctx, status: "stopped", state: operationsState{Desired: "running", Active: app, Version: 1, Operations: map[string]*Operation{}}}
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

	data, err := os.ReadFile(path)
	if err == nil {
		var stored interface{}
		if err = json.Unmarshal(data, &stored); err != nil {
			return nil, err
		}
		stripObsoleteAccountConfig(stored)
		data, err = json.Marshal(stored)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &s.state); err != nil {
			return nil, err
		}
		if s.state.Desired != "running" && s.state.Desired != "paused" && s.state.Desired != "stopped" {
			return nil, fmt.Errorf("invalid persisted desired state")
		}
		if s.state.Version == 0 {
			return nil, fmt.Errorf("invalid persisted configuration version")
		}
		s.state.Active.Config.API = app.Config.API
		s.state.Active.Config.Streamer = app.Config.Streamer
		if err = Validate(s.state.Active); err != nil {
			return nil, err
		}
		if s.state.Operations == nil {
			s.state.Operations = map[string]*Operation{}
		}
		for id, op := range s.state.Operations {
			if op == nil || op.ID != id {
				return nil, fmt.Errorf("invalid persisted operation %s", id)
			}
			if op.State != "accepted" && op.State != "running" && op.State != "completed" && op.State != "failed" {
				return nil, fmt.Errorf("invalid persisted operation state")
			}
			if op.State == "accepted" || op.State == "running" {
				op.State = "failed"
				op.Error = "Interrupted by process restart; inspect runtime before issuing a new command"
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := s.save(); err != nil {
		return nil, err
	}
	success = true
	return s, nil
}
func (s *Supervisor) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(s.path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(s.path+".tmp", s.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (s *Supervisor) start() error {
	if s.done != nil {
		return fmt.Errorf("runtime already exists")
	}
	s.setStatus("starting")
	ctx, cancel := context.WithCancel(s.ctx)
	s.cancel = cancel
	s.control = &runtimeControl{log: &s.log, initialPaused: s.state.Desired == "paused", resourcesDone: make(chan struct{})}
	s.done = make(chan struct{})
	s.result = &runtimeResult{}
	control, done, app, result := s.control, s.done, s.state.Active, s.result
	if s.originGate != nil {
		app.Config.HTTPClient.GlobalGate = s.originGate
		app.Config.HTTPClient.LocalGate = util.NewOriginGate()
	}
	go func() { result.err = Run(context.WithValue(ctx, runtimeKey{}, control), app); close(done) }()
	deadline := time.NewTimer(app.Config.ShutdownTimeout)
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
	case "operations":
		result := map[string]Operation{}
		for id, op := range s.state.Operations {
			copy := *op
			copy.Error = s.safeError(copy.Error)
			result[id] = copy
		}
		return result, nil
	case "draft", "validate":
		for _, op := range s.state.Operations {
			if op.State == "accepted" || op.State == "running" {
				return nil, errs.NewRequestError("wait for the active operation")
			}
		}
		var input struct {
			Configuration json.RawMessage `json:"configuration"`
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &input); err != nil {
				return nil, errs.NewRequestError(err.Error())
			}
		}
		if len(input.Configuration) == 0 {
			return redactConfiguration(s.state), nil
		}

		base := s.state.Active
		if s.state.Draft != nil {
			base = *s.state.Draft
		}
		restored, err := restoreConfiguration(input.Configuration, base)
		if err != nil {
			return nil, errs.NewRequestError(err.Error())
		}
		app, err := DecodeBytes(restored)
		if err != nil {
			return nil, errs.NewRequestError(err.Error())
		}
		if app.Config.API.Address != s.state.Active.Config.API.Address || app.Config.API.DashDBPath != s.state.Active.Config.API.DashDBPath {
			return nil, errs.NewRequestError("API bindings and databases require process restart")
		}

		if !reflect.DeepEqual(app.Config.Streamer, s.state.Active.Config.Streamer) {
			return nil, errs.NewRequestError("Streaming bindings require process restart")
		}
		if !reflect.DeepEqual(app.Config.API, s.state.Active.Config.API) {
			return nil, errs.NewRequestError("API administration settings require process restart")
		}
		if action == "draft" {
			previous := s.state.Draft
			s.state.Draft = &app
			if err = s.save(); err != nil {
				s.state.Draft = previous
				return nil, err
			}
		}
		return map[string]interface{}{"valid": true, "resolved": redactConfiguration(operationsState{Active: app})}, nil
	case "command":
		if s.closing {
			return nil, errs.NewRequestError("service is shutting down")
		}
		var command struct {
			ID     string `json:"id"`
			Action string `json:"action"`
			Queue  string `json:"queue"`
			Record string `json:"record"`
		}
		if err := json.Unmarshal(raw, &command); err != nil {
			return nil, errs.NewRequestError(err.Error())
		}
		if command.ID == "" {
			return nil, errs.NewRequestError("command id required")
		}
		if op := s.state.Operations[command.ID]; op != nil {
			if op.Action != command.Action || op.Queue != command.Queue || op.Record != command.Record {
				return nil, errs.NewRequestError("command id already used")
			}
			copy := *op
			copy.Error = s.safeError(copy.Error)
			return copy, nil
		}
		switch command.Action {
		case "start", "stop", "pause", "resume", "activate", "rollback", "replay", "recrawl":
		default:
			return nil, errs.NewRequestError("unknown command")
		}
		for _, op := range s.state.Operations {
			if op.State == "accepted" || op.State == "running" {
				return nil, errs.NewRequestError("another operation is in progress")
			}
		}
		op := &Operation{Queue: command.Queue, Record: command.Record, ID: command.ID, Action: command.Action, State: "accepted", Updated: time.Now().Unix()}
		s.state.Operations[op.ID] = op
		if err := s.save(); err != nil {
			delete(s.state.Operations, op.ID)
			return nil, err
		}
		result := *op
		s.commands.Add(1)
		go s.execute(command.ID, command.Queue, command.Record)
		return result, nil
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
func (s *Supervisor) execute(id, queueName, record string) {
	defer s.commands.Done()
	s.transition.Lock()
	defer s.transition.Unlock()
	s.mu.Lock()
	op := s.state.Operations[id]
	op.State = "running"
	if err := s.save(); err != nil {
		op.State = "failed"
		op.Error = err.Error()
		s.lastError = err.Error()
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.mu.Lock()
	var err error
	if s.closing {
		op.State = "failed"
		op.Error = "service is shutting down"
		s.save()
		s.mu.Unlock()
		return
	}
	switch op.Action {
	case "start", "resume":
		err = s.setDesired("running")
		if err == nil {
			if s.done == nil {
				err = s.start()
			} else {
				err = s.control.pause(s.ctx, false)
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
		s.setStatus("pausing")
		err = s.setDesired("paused")
		if err == nil {
			ctx, cancel := context.WithTimeout(s.ctx, s.state.Active.Config.ShutdownTimeout)
			control := s.control
			s.mu.Unlock()
			if control == nil {
				err = fmt.Errorf("runtime is stopped")
			} else {
				err = control.pause(ctx, true)
			}
			s.mu.Lock()
			cancel()
			if err == nil {
				s.setStatus("paused")
			}
		}
	case "activate", "rollback":
		target := s.state.Draft
		if op.Action == "rollback" {
			target = s.state.Previous
		}
		if target == nil {
			err = fmt.Errorf("no configuration available")
			break
		}
		if err = Validate(*target); err != nil {
			break
		}
		target.Config.API = s.state.Active.Config.API
		target.Config.Streamer = s.state.Active.Config.Streamer
		oldVersion, oldPrevious := s.state.Version, s.state.Previous
		old := s.state.Active
		if err = s.stop(); err != nil {
			break
		}
		s.state.Active = *target
		s.state.Previous = &old
		s.state.Version++
		if err = s.save(); err == nil && s.state.Desired != "stopped" {
			err = s.start()
		}
		if err != nil {
			activationErr := err
			s.state.Active = old
			s.state.Version = oldVersion
			s.state.Previous = oldPrevious
			if saveErr := s.save(); saveErr != nil {
				err = fmt.Errorf("activation: %v; restoring configuration: %v", activationErr, saveErr)
			}
			if s.done == nil && s.state.Desired != "stopped" {
				if rollbackErr := s.start(); rollbackErr != nil {
					err = fmt.Errorf("activation: %v; rollback: %v", activationErr, rollbackErr)
				}
			}
		}

	case "replay", "recrawl":
		err = s.recoverRecord(queueName, record, op.Action == "recrawl")
	}
	if err != nil {
		op.State = "failed"
		op.Error = err.Error()
		s.lastError = err.Error()
	} else {
		op.State = "completed"
		s.lastError = ""
	}
	op.Updated = time.Now().Unix()
	if saveErr := s.save(); saveErr != nil {
		s.lastError = saveErr.Error()
		op.State = "failed"
		op.Error = saveErr.Error()
	}
	s.mu.Unlock()
}
func (s *Supervisor) controlPause(ctx context.Context) error {
	if s.control == nil {
		return fmt.Errorf("runtime is stopped")
	}
	return s.control.pause(ctx, true)
}
func Serve(ctx context.Context, app config.Application) error {
	if !app.Config.API.Enabled {
		return Run(ctx, app)
	}
	s, err := newSupervisor(ctx, app)
	if err != nil {
		return err
	}
	defer s.Close()
	config.Apply(app.Config)
	server, cleanup, err := api.NewServerWithOperations(app.Config.API, s)
	if err != nil {
		return err
	}
	defer cleanup()
	errors := make(chan error, 2)
	if server != nil {
		listener, e := net.Listen("tcp", server.Addr)
		if e != nil {
			return e
		}
		go func() { errors <- server.Serve(listener) }()
		defer server.Close()
	}
	stream := gateway.NewClientStreamer()
	if app.Config.Streamer.Enabled {
		go func() { errors <- stream.Start() }()
		defer func() {
			if c, ok := stream.(interface{ Close() error }); ok {
				c.Close()
			}
		}()
	}
	s.transition.Lock()
	s.mu.Lock()
	if s.state.Desired != "stopped" {
		err = s.start()

	}
	if err != nil {
		s.lastError = err.Error()
		err = nil
	}
	s.mu.Unlock()
	s.transition.Unlock()
	select {
	case <-ctx.Done():
	case err = <-errors:
		if err == http.ErrServerClosed {
			err = nil
		}
	}
	stopErr := s.Close()
	if err == nil {
		err = stopErr
	}
	return err
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
	s.commands.Wait()
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
	if err := s.save(); err != nil {
		s.state.Desired = old
		return err
	}
	return nil
}

func (s *Supervisor) setStatus(status string) {
	s.status = status
	if s.onStatus != nil {
		s.onStatus(status, time.Now())
	}
}
