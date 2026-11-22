package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/report"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type protectedFixtureResource struct {
	path      string
	failClose *atomic.Bool
}

func (r *protectedFixtureResource) SetProtection(*protection.Policy) {}
func (r *protectedFixtureResource) PurgeMatching(ctx context.Context, match func(string) bool) error {
	return protection.RewriteJSONL(ctx, r.path, match)
}
func (r *protectedFixtureResource) VerifyNoMatches(ctx context.Context, match func(string) bool) error {
	return protection.VerifyJSONL(ctx, r.path, match)
}
func (r *protectedFixtureResource) Close() error {
	if r.failClose.Load() {
		return fmt.Errorf("synthetic close failure")
	}
	return nil
}

func TestProtectionCapabilitiesRejectUndeclaredAndIncompleteParticipants(t *testing.T) {
	workspace, e := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	require.NoError(t, e)
	defer workspace.Close()
	a, e := DefaultApplication(Options{DataDirectory: t.TempDir()})
	require.NoError(t, e)
	a.Config.HTTPClient.Protection = workspace.protection
	factory := func(json.RawMessage) (interface{}, error) { return struct{}{}, nil }
	RegisterResource("protection_capability_fixture", factory)
	defer RegisterResource("protection_capability_fixture", nil)
	a.Resources = []config.Resource{{Name: "fixture", Type: "protection_capability_fixture", Parameters: json.RawMessage(`{}`)}}
	e = Validate(a)
	require.Error(t, e)
	require.Contains(t, e.Error(), "does not declare")
	RegisterResource("protection_capability_fixture", factory, ProtectionSupport{Configuration: func(raw json.RawMessage, _ func(string) bool) (json.RawMessage, error) { return raw, nil }})
	require.NoError(t, Validate(a))
	e = checkProtectionParticipant(struct{}{}, workspace.protection, "resource", "protection_capability_fixture")
	require.Error(t, e)
	require.Contains(t, e.Error(), "runtime enforcement")
	RegisterResource("protection_capability_fixture", factory, ProtectionMetadataOnly)
	require.NoError(t, Validate(a))
	require.NoError(t, checkProtectionParticipant(struct{}{}, workspace.protection, "resource", "protection_capability_fixture"))
}

func TestCustomResourceCleanupAndCloseFailureRemainRetryable(t *testing.T) {
	options := Options{DataDirectory: t.TempDir()}
	workspace, e := NewWorkspace(context.Background(), options)
	require.NoError(t, e)
	defer workspace.Close()
	var fail atomic.Bool
	fail.Store(true)
	RegisterResource("protection_data_fixture", func(raw json.RawMessage) (interface{}, error) {
		var p struct {
			Path string `json:"path"`
		}
		if e := json.Unmarshal(raw, &p); e != nil {
			return nil, e
		}
		return &protectedFixtureResource{path: p.Path, failClose: &fail}, nil
	}, ProtectionSupport{Configuration: func(raw json.RawMessage, match func(string) bool) (json.RawMessage, error) {
		var fields map[string]json.RawMessage
		if e := json.Unmarshal(raw, &fields); e != nil {
			return nil, e
		}
		var uri string
		_ = json.Unmarshal(fields["source"], &uri)
		if match(uri) {
			delete(fields, "source")
		}
		return json.Marshal(fields)
	}})
	defer RegisterResource("protection_data_fixture", nil)
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://bad.test/", "http://good.test/"}
	record := createWorkspaceCrawl(t, workspace, d)
	id := record["id"].(string)
	runID := "custom-purge-run"
	root := workspace.runRoot(id, runID)
	require.NoError(t, os.MkdirAll(root, 0700))
	customPath := filepath.Join(root, "custom.jsonl")
	require.NoError(t, os.WriteFile(customPath, []byte("{\"uri\":\"http://bad.test/\"}\n{\"uri\":\"http://good.test/\"}\n"), 0600))
	a, e := CompileCrawl(root, runID, d)
	require.NoError(t, e)
	params, _ := json.Marshal(map[string]interface{}{"path": customPath, "source": "http://bad.test/", "credential": "http://operator:secret@bad.test/", "integer": json.Number("9007199254740993")})
	a.Resources = append(a.Resources, config.Resource{Name: "custom", Type: "protection_data_fixture", Parameters: params})
	workspace.mu.Lock()
	r := workspace.records[id]
	r.Expert = &a
	r.Runs = append(r.Runs, CrawlRun{ID: runID, Definition: d, Expert: &a, Legacy: true, LogsScoped: true, State: "stopped", Desired: "stopped"})
	require.NoError(t, workspace.saveLocked())
	workspace.mu.Unlock()
	incident, e := workspace.protection.Trigger("http://bad.test/", "synthetic", id, runID, 0)
	require.ErrorIs(t, e, protection.ErrBlocked)
	require.Eventually(t, func() bool {
		i, e := workspace.protection.PrivateIncident(incident.ID)
		return e == nil && i.State == "failed"
	}, 10*time.Second, 20*time.Millisecond)
	failed, e := workspace.protection.PrivateIncident(incident.ID)
	require.NoError(t, e)
	require.Contains(t, failed.Error, "synthetic close failure")
	require.NotEqual(t, "complete", failed.Progress[id+"/"+runID])
	fail.Store(false)
	raw, _ := json.Marshal(map[string]interface{}{"action": "retry", "id": incident.ID})
	_, e = workspace.HandleWorkspace(context.Background(), "protection", raw)
	require.NoError(t, e)
	require.Eventually(t, func() bool {
		i, e := workspace.protection.PrivateIncident(incident.ID)
		return e == nil && (i.State == "completed" || i.State == "failed")
	}, 10*time.Second, 20*time.Millisecond)
	completed, e := workspace.protection.PrivateIncident(incident.ID)
	require.NoError(t, e)
	require.Equal(t, "completed", completed.State, completed.Error)
	data, e := os.ReadFile(customPath)
	require.NoError(t, e)
	require.NotContains(t, string(data), "bad.test")
	require.Contains(t, string(data), "good.test")
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	var custom json.RawMessage
	for _, resource := range workspace.records[id].Expert.Resources {
		if resource.Name == "custom" {
			custom = resource.Parameters
		}
	}
	require.NotContains(t, string(custom), `"source"`)
	require.True(t, strings.Contains(string(custom), "9007199254740993"))
	require.Contains(t, string(custom), "http://operator:secret@bad.test/")
}

type interruptibleProtectionResource struct {
	protectedFixtureResource
	block   *atomic.Bool
	entered chan struct{}
}

func (r *interruptibleProtectionResource) PurgeMatching(ctx context.Context, match func(string) bool) error {
	if r.block.Load() {
		select {
		case r.entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	}
	return r.protectedFixtureResource.PurgeMatching(ctx, match)
}
func TestWorkspaceShutdownResumesInterruptedPurgeWithoutExpandingTargets(t *testing.T) {
	options := Options{DataDirectory: t.TempDir()}
	workspace, e := NewWorkspace(context.Background(), options)
	require.NoError(t, e)
	defer func() { workspace.Close() }()
	var block, failClose atomic.Bool
	block.Store(true)
	entered := make(chan struct{}, 1)
	path := ""
	RegisterResource("protection_interrupt_fixture", func(json.RawMessage) (interface{}, error) {
		return &interruptibleProtectionResource{protectedFixtureResource: protectedFixtureResource{path: path, failClose: &failClose}, block: &block, entered: entered}, nil
	}, ProtectionSupport{Configuration: func(raw json.RawMessage, _ func(string) bool) (json.RawMessage, error) { return raw, nil }})
	defer RegisterResource("protection_interrupt_fixture", nil)
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://bad.test/", "http://ref.test/", "http://good.test/"}
	record := createWorkspaceCrawl(t, workspace, d)
	id := record["id"].(string)
	runID := "interrupted-purge"
	root := workspace.runRoot(id, runID)
	require.NoError(t, os.MkdirAll(root, 0700))
	path = filepath.Join(root, "custom.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("{\"uri\":\"http://bad.test/\"}\n{\"uri\":\"http://good.test/\"}\n"), 0600))
	app, e := CompileCrawl(root, runID, d)
	require.NoError(t, e)
	app.Resources = append(app.Resources, config.Resource{Name: "interrupt", Type: "protection_interrupt_fixture", Parameters: json.RawMessage(`{}`)})
	workspace.mu.Lock()
	workspace.records[id].Runs = append(workspace.records[id].Runs, CrawlRun{ID: runID, Definition: d, Expert: &app, Legacy: true, State: "stopped", Desired: "stopped"})
	require.NoError(t, workspace.saveLocked())
	workspace.mu.Unlock()
	graph := filepath.Join(root, "state.json.report.sqlite")
	ledger, e := report.Open(graph, false)
	require.NoError(t, e)
	require.NoError(t, ledger.Discover(context.Background(), []report.URL{{URI: "http://bad.test/", Origin: "http://ref.test/"}}))
	require.NoError(t, ledger.Close())
	incident, e := workspace.protection.Trigger("http://bad.test/", "synthetic", id, runID, 1)
	require.ErrorIs(t, e, protection.ErrBlocked)
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("purge never reached controlled interruption")
	}
	before, e := workspace.protection.PrivateIncident(incident.ID)
	require.NoError(t, e)
	require.Len(t, before.Targets, 2)
	require.NoError(t, workspace.Close())
	ledger, e = report.Open(graph, false)
	require.NoError(t, e)
	require.NoError(t, ledger.Discover(context.Background(), []report.URL{{URI: "http://bad.test/", Origin: "http://later.test/"}}))
	require.NoError(t, ledger.Close())
	block.Store(false)
	workspace, e = NewWorkspace(context.Background(), options)
	require.NoError(t, e)
	require.Eventually(t, func() bool {
		v, e := workspace.protection.PrivateIncident(incident.ID)
		return e == nil && (v.State == "completed" || v.State == "failed")
	}, 10*time.Second, 20*time.Millisecond)
	after, e := workspace.protection.PrivateIncident(incident.ID)
	require.NoError(t, e)
	require.Equal(t, "completed", after.State, after.Error)
	require.Equal(t, before.Targets, after.Targets)
	require.NoError(t, workspace.protection.Check("http://later.test/"))
	raw, e := os.ReadFile(path)
	require.NoError(t, e)
	require.NotContains(t, string(raw), "bad.test")
	require.Contains(t, string(raw), "good.test")
}
