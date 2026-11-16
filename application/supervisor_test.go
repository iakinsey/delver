package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testSupervisor(t *testing.T) *Supervisor {
	t.Helper()
	app := config.Application{Config: config.LoadConfig()}
	app.Config.API.Enabled = false
	app.Config.Streamer.Enabled = false
	app.Config.Metrics.Enabled = false
	app.Config.API.OperationsPath = filepath.Join(t.TempDir(), "operations.json")
	app.Config.ShutdownTimeout = time.Second
	s, err := newSupervisor(context.Background(), app)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return s
}
func runCommand(t *testing.T, s *Supervisor, id, action string) Operation {
	t.Helper()
	err := s.Execute(context.Background(), action, "", "")
	op := Operation{ID: id, Action: action, State: "completed"}
	if err != nil {
		op.State = "failed"
		op.Error = err.Error()
	}
	return op
}
func TestSupervisorLifecycleAndPersistence(t *testing.T) {
	s := testSupervisor(t)
	for i, action := range []string{"start", "pause", "resume", "stop", "start", "stop"} {
		op := runCommand(t, s, fmt.Sprint(i), action)
		require.Equal(t, "completed", op.State, op.Error)
	}
	_, err := os.Stat(s.path)
	require.True(t, os.IsNotExist(err), "executor must not create a configuration/operation journal")
}
func TestSupervisorDraftValidation(t *testing.T) {
	s := testSupervisor(t)
	for _, action := range []string{"draft", "validate", "command", "operations"} {
		_, err := s.Handle(context.Background(), action, json.RawMessage(`{}`))
		require.Error(t, err)
	}
	require.Equal(t, "stopped", s.status)
}
func TestPausedStateRestoresWithoutConsumingWork(t *testing.T) {
	root := t.TempDir()
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://127.0.0.1:1/"}
	r := createWorkspaceCrawl(t, w, d)
	startWorkspaceCrawl(t, w, r)
	workspaceCall(t, w, "command", map[string]interface{}{"crawl_id": r["id"], "id": "pause", "action": "pause"})
	paused := waitWorkspace(t, w, r["id"].(string), func(v map[string]interface{}) bool { return runtimeState(v) == "paused" })
	elapsed := latestRun(paused)["elapsed_ns"]
	require.NoError(t, w.Close())
	w, err = NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	defer w.Close()
	restored := waitWorkspace(t, w, r["id"].(string), func(v map[string]interface{}) bool { return runtimeState(v) == "paused" })
	require.Equal(t, elapsed, latestRun(restored)["elapsed_ns"])
}
func TestConfigurationPreservesIntegerPrecision(t *testing.T) {
	app, err := DecodeBytes([]byte(`{"config":{"shutdown_timeout":9007199254740993}}`))
	require.NoError(t, err)
	require.EqualValues(t, 9007199254740993, app.Config.ShutdownTimeout)
}

func TestSupervisorShutdownRejectsCommands(t *testing.T) {
	s := testSupervisor(t)
	require.NoError(t, s.Close())
	_, err := s.Handle(context.Background(), "command", json.RawMessage(`{"id":"late","action":"start"}`))
	require.Error(t, err)
}
func TestInterruptedOperationIsNotRepeated(t *testing.T) {
	root := t.TempDir()
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://example.test/"}
	r := createWorkspaceCrawl(t, w, d)
	w.mu.Lock()
	w.records[r["id"].(string)].Operations = map[string]*Operation{"interrupted": {ID: "interrupted", Action: "start_run", State: "running"}}
	require.NoError(t, w.saveLocked())
	w.mu.Unlock()
	require.NoError(t, w.Close())
	restored, err := NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	defer restored.Close()
	require.Equal(t, "failed", restored.records[r["id"].(string)].Operations["interrupted"].State)
	require.Empty(t, restored.records[r["id"].(string)].Runs)
}
func TestOperationsStateHasExclusiveOwner(t *testing.T) {
	s := testSupervisor(t)
	_, err := NewSupervisor(context.Background(), s.state.Active)
	require.Error(t, err)
}

func TestDecodeRejectsTrailingAndOversizedInput(t *testing.T) {
	for _, raw := range []string{`{} {}`, `{} trailing`, "{}" + strings.Repeat(" ", 4<<20)} {
		_, err := DecodeBytes([]byte(raw))
		require.Error(t, err)
	}
}
func TestCustomParametersKeepTheirOwnDurationSemantics(t *testing.T) {
	RegisterResource("arbitrary_fixture", func(json.RawMessage) (interface{}, error) { return struct{}{}, nil })
	defer RegisterResource("arbitrary_fixture", nil)
	app, err := DecodeBytes([]byte(`{"resources":[{"name":"custom","type":"arbitrary_fixture","parameters":{"interval":"custom expression","timeout":"library-specific"}}]}`))
	require.NoError(t, err)
	require.Contains(t, string(app.Resources[0].Parameters), "custom expression")
}

func TestTimerOutputAndInvalidProxyFailPreflight(t *testing.T) {
	for _, raw := range []string{
		`{"config":{"http_client":{"http_proxy_url":"not-a-proxy"}}}`,
		`{"config":{"http_client":{"socks5_url":"missing-port"}}}`,
		`{"resources":[{"name":"timer","type":"timer","parameters":{"delay":"1s"}}],"workers":[{"name":"seed","type":"fixed_seed_publisher","manager":"job","interval":"1h","outbox":"timer","parameters":{"uris":["http://example.test/"]}}]}`,
	} {
		_, err := DecodeBytes([]byte(raw))
		require.Error(t, err)
	}
}

func TestObsoleteAccountSettingsAreRemovedDuringUpgrade(t *testing.T) {
	app, err := DecodeBytes([]byte(`{"config":{"api":{"user_db_path":"unused.db","admin_emails":["obsolete@example.test"]}}}`))
	require.NoError(t, err)
	data, err := json.Marshal(app)
	require.NoError(t, err)
	require.NotContains(t, string(data), "user_db_path")
	require.NotContains(t, string(data), "admin_emails")
}

func TestPersistedStateUpgradeRemovesAccountFields(t *testing.T) {
	root := t.TempDir()
	app := config.Application{Config: config.LoadConfig()}
	app.Config.API.OperationsPath = filepath.Join(root, "state.json")
	app.Resources = []config.Resource{{Name: "in", Type: "channel_queue"}}
	app.Workers = []config.Worker{{Name: "source", Type: "fixed_seed_publisher", Manager: "job", Interval: time.Hour, Outbox: "in", Parameters: json.RawMessage(`{"uris":["http://example.test/"]}`)}}
	state := operationsState{Desired: "stopped", Active: app, Version: 1}
	raw, _ := json.Marshal(state)
	var document map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &document))
	settings := document["active"].(map[string]interface{})["config"].(map[string]interface{})["api"].(map[string]interface{})
	settings["user_db_path"] = "unused.db"
	settings["admin_emails"] = []string{"old@example.test"}
	raw, _ = json.Marshal(document)
	require.NoError(t, os.WriteFile(app.Config.API.OperationsPath, raw, 0600))
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	defer w.Close()
	raw, err = os.ReadFile(filepath.Join(root, "crawls.json"))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "admin_emails")
	require.NotContains(t, string(raw), "user_db_path")
}

func (s *Supervisor) save() error {
	data, err := json.Marshal(operationsState{Desired: s.state.Desired, Active: s.state.Active, Version: s.state.Version})
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}
func controlWorkspace(t *testing.T) (*Workspace, map[string]interface{}) {
	t.Helper()
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { w.Close() })
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://127.0.0.1:1/"}
	return w, createWorkspaceCrawl(t, w, d)
}
