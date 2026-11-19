package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/flow"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceExecutionSettingsRevisionPersistenceAndIsolation(t *testing.T) {
	root := t.TempDir()
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	initial, err := w.HandleWorkspace(context.Background(), "settings", nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), initial.(WorkspaceSettings).Revision)
	raw := json.RawMessage(`{"revision":1,"execution":{"concurrency":7,"host_concurrency":3,"host_delay":"20ms"}}`)
	updated, err := w.HandleWorkspace(context.Background(), "settings", raw)
	require.NoError(t, err)
	require.Equal(t, uint64(2), updated.(WorkspaceSettings).Revision)
	require.Equal(t, 7, w.broker.Limits().Concurrency)
	_, err = w.HandleWorkspace(context.Background(), "settings", raw)
	require.Error(t, err)
	require.Contains(t, err.Error(), "revision conflict")
	for _, value := range []string{`{"revision":2,"execution":{"concurrency":0,"host_concurrency":2,"host_delay":"0s"}}`, `{"revision":2,"execution":{"concurrency":4,"host_concurrency":0,"host_delay":"0s"}}`, `{"revision":2,"execution":{"concurrency":4,"host_concurrency":2,"host_delay":"-1s"}}`} {
		_, err = w.HandleWorkspace(context.Background(), "settings", json.RawMessage(value))
		require.Error(t, err)
		require.Equal(t, uint64(2), w.settings.Revision)
	}
	require.NoError(t, w.Close())
	w, err = NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	defer w.Close()
	require.Equal(t, 7, w.broker.Limits().Concurrency)
	other, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	require.NoError(t, err)
	defer other.Close()
	require.Equal(t, flow.DefaultExecutionLimits(), other.broker.Limits())
	require.NotSame(t, w.gate, other.gate)
}
func TestWorkspaceSettingsWriteFailureDoesNotApplyLimits(t *testing.T) {
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	require.NoError(t, err)
	defer w.Close()
	before := w.broker.Limits()
	require.NoError(t, os.Mkdir(filepath.Join(w.root, "crawls.json.tmp"), 0700))
	_, err = w.HandleWorkspace(context.Background(), "settings", json.RawMessage(`{"revision":1,"execution":{"concurrency":1,"host_concurrency":1,"host_delay":"1s"}}`))
	require.Error(t, err)
	require.Equal(t, before, w.broker.Limits())
	require.Equal(t, uint64(1), w.settings.Revision)
	require.NoError(t, os.Remove(filepath.Join(w.root, "crawls.json.tmp")))
}
func TestWorkspaceRegistryMigrationConservativelyImportsSharedLimits(t *testing.T) {
	root := t.TempDir()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://example.test/"}
	one := CrawlRecord{ID: "first", Definition: d, Revision: 1}
	two := one
	two.ID = "second"
	data, _ := json.Marshal(map[string]*CrawlRecord{"first": &one, "second": &two})
	var doc map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &doc))
	for id, values := range map[string][]interface{}{"first": {12, 3, "20ms"}, "second": {8, 2, "50ms"}} {
		f := doc[id].(map[string]interface{})["definition"].(map[string]interface{})["fetch"].(map[string]interface{})
		f["process_limit"] = values[0]
		f["process_origin_limit"] = values[1]
		f["process_origin_delay"] = values[2]
	}
	data, _ = json.Marshal(doc)
	require.NoError(t, os.WriteFile(filepath.Join(root, "crawls.json"), data, 0600))
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	require.Equal(t, 8, w.settings.Execution.Concurrency)
	require.Equal(t, 2, w.settings.Execution.HostConcurrency)
	require.Equal(t, "50ms", w.settings.Execution.HostDelay.Value().String())
	require.NoError(t, w.Close())
	backup, err := os.ReadFile(filepath.Join(root, "migration-backups/crawls-v1.json"))
	require.NoError(t, err)
	require.Equal(t, data, backup)
	w, err = NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	defer w.Close()
	require.Equal(t, 8, w.broker.Limits().Concurrency)
}
func TestMigratedRunOperationIDsRemainIdempotent(t *testing.T) {
	root := t.TempDir()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://127.0.0.1:1/"}
	run := CrawlRun{ID: "run-id", State: "stopped", Definition: d, Version: 1}
	record := CrawlRecord{ID: "crawl-id", Revision: 1, Definition: d, Runs: []CrawlRun{run}}
	raw, _ := json.Marshal(map[string]*CrawlRecord{record.ID: &record})
	require.NoError(t, os.WriteFile(filepath.Join(root, "crawls.json"), raw, 0600))
	runRoot := filepath.Join(root, "crawls", record.ID, "runs", run.ID)
	require.NoError(t, os.MkdirAll(runRoot, 0700))
	app, err := CompileCrawl(runRoot, run.ID, d)
	require.NoError(t, err)
	old := operationsState{Desired: "stopped", Active: app, Version: 1, Operations: map[string]*Operation{"old-stop": {ID: "old-stop", Action: "stop", State: "completed", Version: 1}}}
	raw, _ = json.Marshal(old)
	require.NoError(t, os.WriteFile(filepath.Join(runRoot, "state.json"), raw, 0600))
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	defer w.Close()
	value := workspaceCall(t, w, "command", map[string]interface{}{"crawl_id": record.ID, "id": "old-stop", "action": "stop"}).(map[string]interface{})
	require.Equal(t, "old-stop", value["id"])
	require.Equal(t, "completed", value["state"])
}
func TestHistoricalConfigurationRetainsRetiredFieldsExactly(t *testing.T) {
	var run CrawlRun
	raw := []byte(`{"id":"run-id","state":"stopped","version":1,"definition":{"name":"history","fetch":{"mode":"fixed","concurrency":4,"minimum":1,"maximum":8,"sample_interval":"1s","evaluation_window":"30s","adjustment_cooldown":"45s","min_samples":20,"increase_step":1,"decrease_factor":0.8,"ewma_alpha":0.2,"latency_inflation":1.5,"minimum_gain":0.02,"overload_failure_ratio":0.2,"memory_high_mb":4096,"memory_low_mb":3072,"cpu_high_percent":90,"cpu_low_percent":70,"min_free_disk_mb":64,"queue_low_multiplier":1,"queue_target_multiplier":2,"queue_hard_limit":4096,"refill_batch":256,"candidate_window":4096,"downstream_high":128,"downstream_low":64,"failure_threshold":3,"host_cooldown":"30s","host_cooldown_max":"30m","recovery_probe_interval":"30s","retry_share":0.2,"metrics_window":"1m","metrics_samples":1024,"metrics_save_interval":"10s"}}}`)
	require.NoError(t, json.Unmarshal(raw, &run))
	require.Contains(t, string(run.ConfigurationSnapshot), `"ewma_alpha":0.2`)
	require.Equal(t, flow.Duration(45*time.Second), run.Definition.Fetch.AdjustmentInterval)
	saved, err := json.Marshal(run)
	require.NoError(t, err)
	var again CrawlRun
	require.NoError(t, json.Unmarshal(saved, &again))
	require.JSONEq(t, string(run.ConfigurationSnapshot), string(again.ConfigurationSnapshot))
}
func TestInvalidRegistryDoesNotActivateOtherCrawls(t *testing.T) {
	root := t.TempDir()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://127.0.0.1:1/"}
	records := map[string]*CrawlRecord{"valid": {ID: "valid", Revision: 1, Definition: d, Runs: []CrawlRun{{ID: "valid-run", Definition: d, Desired: "running", State: "running"}}}, "invalid": {ID: "mismatched", Revision: 1, Definition: d}}
	raw, _ := json.Marshal(records)
	require.NoError(t, os.WriteFile(filepath.Join(root, "crawls.json"), raw, 0600))
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.Error(t, err)
	require.Nil(t, w)
	_, err = os.Stat(filepath.Join(root, "crawls/valid"))
	require.True(t, os.IsNotExist(err), "invalid registry must fail before opening runtime storage")
}

func TestMigrationPersistenceFailureDoesNotOpenRuntime(t *testing.T) {
	root := t.TempDir()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://127.0.0.1:1/"}
	records := map[string]*CrawlRecord{"crawl-id": {ID: "crawl-id", Revision: 1, Definition: d, Runs: []CrawlRun{{ID: "run-id", Definition: d, State: "running", Desired: "running"}}}}
	raw, err := json.Marshal(records)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "crawls.json"), raw, 0600))
	require.NoError(t, os.Mkdir(filepath.Join(root, "crawls.json.tmp"), 0700))
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.Error(t, err)
	require.Nil(t, w)
	original, err := os.ReadFile(filepath.Join(root, "crawls.json"))
	require.NoError(t, err)
	require.Equal(t, raw, original)
	backup, err := os.ReadFile(filepath.Join(root, "migration-backups/crawls-v1.json"))
	require.NoError(t, err)
	require.Equal(t, raw, backup)
	_, err = os.Stat(filepath.Join(root, "crawls/crawl-id"))
	require.True(t, os.IsNotExist(err))
}
func TestUnpersistedOperationCannotExecuteAndSameIDCanRetry(t *testing.T) {
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	require.NoError(t, err)
	defer w.Close()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://127.0.0.1:1/"}
	record := createWorkspaceCrawl(t, w, d)
	id := record["id"].(string)
	require.NoError(t, os.Mkdir(filepath.Join(w.root, "crawls.json.tmp"), 0700))
	raw, _ := json.Marshal(map[string]interface{}{"crawl_id": id, "id": "durable-start", "revision": record["revision"]})
	_, err = w.HandleCrawls(context.Background(), "start_run", raw)
	require.Error(t, err)
	require.Empty(t, w.records[id].Runs)
	require.Empty(t, w.records[id].Operations)
	require.NoError(t, os.Remove(filepath.Join(w.root, "crawls.json.tmp")))
	_, err = w.HandleCrawls(context.Background(), "start_run", raw)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		op := w.records[id].Operations["durable-start"]
		return op != nil && (op.State == "completed" || op.State == "failed")
	}, 5*time.Second, 10*time.Millisecond)
	w.mu.Lock()
	require.Len(t, w.records[id].Runs, 1)
	require.Len(t, w.records[id].Operations, 1)
	w.mu.Unlock()
}

func TestFailedArchiveAndRunSaveRetainConfigurationAndRevision(t *testing.T) {
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	require.NoError(t, err)
	defer w.Close()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://127.0.0.1:1/"}
	record := createWorkspaceCrawl(t, w, d)
	id := record["id"].(string)
	require.NoError(t, os.Mkdir(filepath.Join(w.root, "crawls.json.tmp"), 0700))
	raw, _ := json.Marshal(map[string]interface{}{"crawl_id": id, "revision": record["revision"]})
	_, err = w.HandleCrawls(context.Background(), "archive", raw)
	require.Error(t, err)
	require.False(t, w.records[id].Archived)
	require.Equal(t, uint64(1), w.records[id].Revision)
	draft := d
	draft.Name = "pending draft"
	w.records[id].Draft = &draft
	_, err = w.applyRun(w.records[id], w.slots[id], "activate", 1)
	require.Error(t, err)
	require.Empty(t, w.records[id].Runs)
	require.Equal(t, d.Name, w.records[id].Definition.Name)
	require.NotNil(t, w.records[id].Draft)
	require.Equal(t, "pending draft", w.records[id].Draft.Name)
	require.Equal(t, uint64(1), w.records[id].Revision)
	require.NoError(t, os.Remove(filepath.Join(w.root, "crawls.json.tmp")))
	require.NoError(t, w.Close())
	reopened, err := NewWorkspace(context.Background(), Options{DataDirectory: w.root})
	require.NoError(t, err)
	defer reopened.Close()
	require.Empty(t, reopened.records[id].Runs)
	require.False(t, reopened.records[id].Archived)
	require.Equal(t, "pending draft", reopened.records[id].Draft.Name)
}

func TestExecutionSettingsCanChangeDuringRunRestart(t *testing.T) {
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	require.NoError(t, err)
	defer w.Close()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://127.0.0.1:1/"}
	d.Fetch.Concurrency = 1
	d.Fetch.Maximum = 2
	record := createWorkspaceCrawl(t, w, d)
	id := record["id"].(string)
	stop, done := make(chan struct{}), make(chan error, 1)
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				done <- nil
				return
			case <-time.After(time.Millisecond):
			}
			current, e := w.HandleWorkspace(context.Background(), "settings", nil)
			if e != nil {
				done <- e
				return
			}
			raw, _ := json.Marshal(map[string]interface{}{"revision": current.(WorkspaceSettings).Revision, "execution": flow.ExecutionLimits{Concurrency: 16 + i%2, HostConcurrency: 2}})
			if _, e = w.HandleWorkspace(context.Background(), "settings", raw); e != nil {
				done <- e
				return
			}
		}
	}()
	defer func() { close(stop); require.NoError(t, <-done) }()
	for i := 0; i < 3; i++ {
		startWorkspaceCrawl(t, w, record)
		workspaceCall(t, w, "command", map[string]interface{}{"crawl_id": id, "id": fmt.Sprint("stop-", i), "action": "stop"})
		record = waitWorkspace(t, w, id, func(v map[string]interface{}) bool { return runtimeState(v) == "stopped" })
	}
	require.Len(t, w.records[id].Runs, 1)
}

func TestLegacyImporterPreservesUnactivatedDraftAndConfigurationRevision(t *testing.T) {
	legacy := t.TempDir()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://127.0.0.1:1/"}
	app, err := CompileCrawl(legacy, "legacy-run", d)
	require.NoError(t, err)
	app.Config.API.OperationsPath = filepath.Join(legacy, "state.json")
	raw, _ := json.Marshal(app)
	draft, err := DecodeBytes(raw)
	require.NoError(t, err)
	draft.Config.HTTPClient.UserAgent = "legacy-draft-agent"
	state := operationsState{Desired: "stopped", Version: 7, Active: app, Draft: &draft}
	raw, _ = json.Marshal(state)
	require.NoError(t, os.WriteFile(app.Config.API.OperationsPath, raw, 0600))
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir(), initial: &app})
	require.NoError(t, err)
	defer w.Close()
	require.Len(t, w.records, 1)
	for _, record := range w.records {
		require.Equal(t, uint64(7), record.Revision)
		require.Equal(t, uint64(7), record.Runs[0].Version)
		require.Equal(t, "stopped", record.Runs[0].Desired)
		require.NotNil(t, record.ExpertDraft)
		require.NotNil(t, record.Draft)
		require.Equal(t, "legacy-draft-agent", record.ExpertDraft.Config.HTTPClient.UserAgent)
		require.NotEqual(t, "legacy-draft-agent", record.Expert.Config.HTTPClient.UserAgent)
		backup, err := os.ReadFile(filepath.Join(w.root, "migration-backups", record.ID+"-"+record.Runs[0].ID+"-state.json"))
		require.NoError(t, err)
		require.Equal(t, raw, backup)
	}
}

func TestRunCommandIdempotencySurvivesDraftRevisionsAndRestart(t *testing.T) {
	root := t.TempDir()
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	defer func() {
		if w != nil {
			w.Close()
		}
	}()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://127.0.0.1:1/"}
	d.Fetch.Concurrency = 1
	d.Fetch.Maximum = 2
	record := createWorkspaceCrawl(t, w, d)
	id := record["id"].(string)
	startWorkspaceCrawl(t, w, record)
	command := json.RawMessage(fmt.Sprintf(`{"crawl_id":%q,"id":"stable-stop","action":"stop"}`, id))
	_, err = w.HandleCrawls(context.Background(), "command", command)
	require.NoError(t, err)
	stopped := waitWorkspace(t, w, id, func(v map[string]interface{}) bool { return runtimeState(v) == "stopped" })
	d.Name = "saved draft"
	workspaceCall(t, w, "draft", map[string]interface{}{"crawl_id": id, "revision": stopped["revision"], "definition": d})
	before := len(w.records[id].Operations)
	value, err := w.HandleCrawls(context.Background(), "command", command)
	require.NoError(t, err)
	raw, _ := json.Marshal(value)
	var operation Operation
	require.NoError(t, json.Unmarshal(raw, &operation))
	require.Equal(t, "stable-stop", operation.ID)
	require.Equal(t, "completed", operation.State)
	require.Len(t, w.records[id].Operations, before)
	changed := json.RawMessage(fmt.Sprintf(`{"crawl_id":%q,"id":"stable-stop","action":"resume"}`, id))
	_, err = w.HandleCrawls(context.Background(), "command", changed)
	require.Error(t, err)
	require.NoError(t, w.Close())
	w, err = NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	_, err = w.HandleCrawls(context.Background(), "command", command)
	require.NoError(t, err)
	require.Len(t, w.records[id].Operations, before)
	require.Equal(t, "stopped", w.records[id].Runs[0].Desired)
}

func TestSharedLimitMigrationIgnoresOpaqueComponentParameters(t *testing.T) {
	root := t.TempDir()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://example.test/"}
	expert := config.Application{Config: config.LoadConfig(), Workers: []config.Worker{{Name: "custom", Type: "opaque_fixture", Parameters: json.RawMessage(`{"fetch":{"process_limit":1,"process_origin_limit":1,"process_origin_delay":"24h"},"opaque":9007199254740993123}`)}}}
	record := CrawlRecord{ID: "custom-crawl", Revision: 1, Definition: d, Expert: &expert}
	raw, _ := json.Marshal(map[string]*CrawlRecord{record.ID: &record})
	var legacy map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &legacy))
	definition := legacy[record.ID].(map[string]interface{})["definition"].(map[string]interface{})
	definition["fetch"].(map[string]interface{})["process_limit"] = 12
	// Preserve the opaque large integer by encoding the original expert again.
	legacy[record.ID].(map[string]interface{})["expert"] = &expert
	raw, _ = json.Marshal(legacy)
	require.NoError(t, os.WriteFile(filepath.Join(root, "crawls.json"), raw, 0600))
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.NoError(t, err)
	defer w.Close()
	require.Equal(t, 12, w.settings.Execution.Concurrency)
	require.Equal(t, 2, w.settings.Execution.HostConcurrency)
	require.Equal(t, flow.Duration(100*time.Millisecond), w.settings.Execution.HostDelay)
	require.Contains(t, string(w.records[record.ID].Expert.Workers[0].Parameters), "9007199254740993123")
}

func TestWorkspaceMigrationIgnoresRuntimeFetchMetrics(t *testing.T) {
	for _, limit := range []int{0, 1, 8192} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			root := t.TempDir()
			d := DefaultCrawlDefinition()
			d.Seeds = []string{"http://example.test/"}
			record := CrawlRecord{ID: "crawl-id", Revision: 1, Definition: d, Runs: []CrawlRun{{ID: "run-id", Version: 1, State: "stopped", Desired: "stopped", Definition: d, Summary: &RuntimeSnapshot{Fetch: &flow.Snapshot{ProcessLimit: limit}}}}}
			data, err := json.Marshal(map[string]*CrawlRecord{record.ID: &record})
			require.NoError(t, err)
			var doc map[string]interface{}
			require.NoError(t, json.Unmarshal(data, &doc))
			f := doc[record.ID].(map[string]interface{})["definition"].(map[string]interface{})["fetch"].(map[string]interface{})
			f["process_limit"] = 12
			f["process_origin_limit"] = 3
			f["process_origin_delay"] = "20ms"
			data, err = json.Marshal(doc)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(root, "crawls.json"), data, 0600))
			w, err := NewWorkspace(context.Background(), Options{DataDirectory: root})
			require.NoError(t, err)
			require.Equal(t, 12, w.settings.Execution.Concurrency)
			require.Equal(t, 3, w.settings.Execution.HostConcurrency)
			require.Equal(t, "20ms", w.settings.Execution.HostDelay.Value().String())
			require.Equal(t, limit, w.records[record.ID].Runs[0].Summary.Fetch.ProcessLimit)
			require.NoError(t, w.Close())
			backup, err := os.ReadFile(filepath.Join(root, "migration-backups/crawls-v1.json"))
			require.NoError(t, err)
			require.Equal(t, data, backup)
			w, err = NewWorkspace(context.Background(), Options{DataDirectory: root})
			require.NoError(t, err)
			defer w.Close()
			require.Equal(t, 12, w.settings.Execution.Concurrency)
			require.Equal(t, limit, w.records[record.ID].Runs[0].Summary.Fetch.ProcessLimit)
		})
	}
}

func TestWorkspaceMigrationCollectsEverySavedConfigurationSection(t *testing.T) {
	for _, section := range []string{"definition", "draft", "expert", "expert_draft", "run_definition", "run_expert"} {
		t.Run(section, func(t *testing.T) {
			d := DefaultCrawlDefinition()
			d.Seeds = []string{"http://example.test/"}
			definition, _ := json.Marshal(d)
			var def map[string]interface{}
			require.NoError(t, json.Unmarshal(definition, &def))
			f := def["fetch"].(map[string]interface{})
			f["process_limit"] = 7
			f["process_origin_limit"] = 3
			f["process_origin_delay"] = "25ms"
			record := map[string]interface{}{"id": "crawl-id", "revision": 1, "definition": d, "runs": []interface{}{map[string]interface{}{"id": "run-id", "version": 1, "state": "stopped", "desired": "stopped", "definition": d}}}
			pipeline := config.Application{Config: config.LoadConfig()}
			pipeline.Config.Fetch = d.Fetch
			raw, _ := json.Marshal(pipeline)
			var expert map[string]interface{}
			require.NoError(t, json.Unmarshal(raw, &expert))
			expert["config"].(map[string]interface{})["fetch"] = f
			run := record["runs"].([]interface{})[0].(map[string]interface{})
			switch section {
			case "definition", "draft":
				record[section] = def
			case "expert", "expert_draft":
				record[section] = expert
			case "run_definition":
				run["definition"] = def
			case "run_expert":
				run["expert"] = expert
			}
			// Snapshots and unrelated metadata cannot override actual configuration.
			run["configuration_snapshot"] = map[string]interface{}{"definition": map[string]interface{}{"fetch": map[string]interface{}{"process_limit": 1, "process_origin_limit": 1, "process_origin_delay": "24h"}}}
			record["diagnostic"] = map[string]interface{}{"fetch": map[string]interface{}{"process_limit": 0}}
			raw, err := json.Marshal(map[string]interface{}{"crawl-id": record})
			require.NoError(t, err)
			w := &Workspace{root: t.TempDir(), settings: WorkspaceSettings{Revision: 1, Execution: flow.DefaultExecutionLimits()}}
			require.NoError(t, w.loadRegistry(raw))
			require.Equal(t, 7, w.settings.Execution.Concurrency)
			require.Equal(t, 3, w.settings.Execution.HostConcurrency)
			require.Equal(t, "25ms", w.settings.Execution.HostDelay.Value().String())
		})
	}
}
