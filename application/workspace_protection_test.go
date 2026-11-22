package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/report"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceRestrictedPurgeExistingRunAndResume(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<title>synthetic fixture</title>")
	}))
	defer proxy.Close()
	options := Options{DataDirectory: t.TempDir()}
	w, e := NewWorkspace(context.Background(), options)
	require.NoError(t, e)
	defer func() { w.Close() }()
	d := DefaultCrawlDefinition()
	d.Name = "purge fixture"
	d.Scope = "open_web"
	d.Seeds = []string{"http://bad.test/page", "http://ref.test/page", "http://good.test/page"}
	d.Proxy = ProxySettings{Mode: "http", URL: proxy.URL}
	no := false
	d.Network.RespectRobots = &no
	d.Termination.WhenDrained = true
	d.RequestDelay = "0s"
	r := createWorkspaceCrawl(t, w, d)
	startWorkspaceCrawl(t, w, r)
	id := r["id"].(string)
	detail := waitWorkspace(t, w, id, func(v map[string]interface{}) bool { return latestRun(v)["state"] == "completed" })
	run := latestRun(detail)["id"].(string)
	root := w.runRoot(id, run)
	// Model an unclean process exit during an earlier, unrelated purge.
	stale, e := os.CreateTemp(root, ".purge-*")
	require.NoError(t, e)
	_, e = stale.WriteString("{\"uri\":\"http://bad.test/page\",\"text\":\"synthetic\"}\n")
	require.NoError(t, e)
	require.NoError(t, stale.Close())
	ledger, e := report.Open(filepath.Join(root, "state.json.report.sqlite"), false)
	require.NoError(t, e)
	require.NoError(t, ledger.Discover(context.Background(), []report.URL{{URI: "http://bad.test/page", Origin: "http://ref.test/page"}, {URI: "http://good.test/page", Origin: "http://bad.test/page"}}))
	ledger.Close()
	_, e = w.protection.Trigger("http://bad.test/page", "fixture", id, run, 1)
	require.ErrorIs(t, e, protection.ErrBlocked)
	require.Eventually(t, func() bool {
		s := w.protection.Snapshot()
		return len(s.Incidents) > 0 && (s.Incidents[0].State == "completed" || s.Incidents[0].State == "failed")
	}, 8*time.Second, 20*time.Millisecond)
	incident := w.protection.Snapshot().Incidents[0]
	require.Equal(t, "completed", incident.State, incident.Error)
	_, e = os.Stat(stale.Name())
	require.True(t, os.IsNotExist(e), "completed purge retained an abandoned response copy")
	require.Len(t, incident.Targets, 2)
	data, e := os.ReadFile(filepath.Join(root, "results.jsonl"))
	require.NoError(t, e)
	require.NotContains(t, string(data), "bad.test")
	require.NotContains(t, string(data), "ref.test")
	require.Contains(t, string(data), "good.test")
	registry, e := os.ReadFile(filepath.Join(w.root, "crawls.json"))
	require.NoError(t, e)
	require.NotContains(t, string(registry), "bad.test")
	require.NotContains(t, string(registry), "ref.test")
	require.NoError(t, w.Close())
	w, e = NewWorkspace(context.Background(), options)
	require.NoError(t, e)
	require.ErrorIs(t, w.protection.Check("http://sub.ref.test/"), protection.ErrBlocked)
	require.NoError(t, w.protection.Check("http://good.test/"))
}
func TestWorkspaceListPreviewRevisionAndAtomicValidation(t *testing.T) {
	w, e := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	require.NoError(t, e)
	defer w.Close()
	call := func(p interface{}) (interface{}, error) {
		b, _ := json.Marshal(p)
		return w.HandleWorkspace(context.Background(), "protection", b)
	}
	v, e := call(map[string]interface{}{"action": "preview", "revision": 1, "list": map[string]interface{}{"name": "fixture", "text": "example.test\n# note\nhttps://other.test/page", "refresh": "6h", "enabled": true}})
	require.NoError(t, e)
	token := v.(map[string]interface{})["token"]
	_, e = call(map[string]interface{}{"action": "apply", "token": token, "revision": 1, "confirmed": false})
	require.Error(t, e)
	_, e = call(map[string]interface{}{"action": "apply", "token": token, "revision": 1, "confirmed": true})
	require.NoError(t, e)
	require.ErrorIs(t, w.protection.Check("http://a.example.test/"), protection.ErrBlocked)
	_, e = call(map[string]interface{}{"action": "preview", "revision": 2, "list": map[string]interface{}{"name": "bad", "text": "example.test\n*.bad.test", "refresh": "6h"}})
	require.Error(t, e)
	require.True(t, strings.Contains(e.Error(), "line 2"))
	require.EqualValues(t, 2, w.protection.Snapshot().Revision)
}

func TestPurgeSourcesPreservesProxyAndOperationPointers(t *testing.T) {
	d := DefaultCrawlDefinition()
	d.Name = "bad.test"
	d.Seeds = []string{"http://bad.test/page", "http://good.test/page"}
	d.Proxy = ProxySettings{Mode: "http", URL: "http://bad.test:8080"}
	op := &Operation{ID: "operation", State: "running"}
	r := &CrawlRecord{Definition: d, Operations: map[string]*Operation{"operation": op}}
	r.Runs = []CrawlRun{{ID: "run", Definition: d, ConfigurationSnapshot: configurationSnapshot(d, nil, flow.DefaultExecutionLimits())}}
	match := protection.NewMatcher([]protection.Entry{{Value: "bad.test", Domain: true}}, nil).Matches
	require.NoError(t, purgeRecordSources(r, match))
	require.Equal(t, "bad.test", r.Definition.Name)
	require.Equal(t, "http://bad.test:8080", r.Definition.Proxy.URL)
	require.Equal(t, []string{"http://good.test/page"}, r.Definition.Seeds)
	require.Same(t, op, r.Operations["operation"])
	var snapshot struct {
		Definition CrawlDefinition `json:"definition"`
	}
	require.NoError(t, json.Unmarshal(r.Runs[0].ConfigurationSnapshot, &snapshot))
	require.Equal(t, d.Proxy, snapshot.Definition.Proxy)
	require.Equal(t, []string{"http://good.test/page"}, snapshot.Definition.Seeds)
}
func TestWorkspaceRejectsReaddingBlockedSources(t *testing.T) {
	w, e := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	require.NoError(t, e)
	defer w.Close()
	entries, _ := protection.ParseList("blocked-source.test")
	require.NoError(t, w.protection.Update(1, []protection.List{{ID: "test", Name: "test", Refresh: "6h", Enabled: true, Updated: time.Now(), Entries: entries}}, nil))
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://blocked-source.test/"}
	raw, _ := json.Marshal(map[string]interface{}{"definition": d})
	_, e = w.HandleCrawls(context.Background(), "create", raw)
	require.ErrorIs(t, e, protection.ErrBlocked)
	w.mu.Lock()
	require.Empty(t, w.records)
	w.mu.Unlock()
}

func TestWorkspacePurgeDepthAcrossCrawlsAndRestart(t *testing.T) {
	for depth := 0; depth <= 3; depth++ {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, "<title>Harmless synthetic page</title>")
			}))
			defer proxy.Close()
			options := Options{DataDirectory: t.TempDir()}
			workspace, e := NewWorkspace(context.Background(), options)
			require.NoError(t, e)
			defer func() { workspace.Close() }()
			ids := []string{}
			runs := []string{}
			for n := 0; n < 2; n++ {
				d := DefaultCrawlDefinition()
				d.Name = fmt.Sprintf("depth-%d-copy-%d", depth, n)
				d.Scope = "open_web"
				d.Seeds = []string{"http://bad.test/", "http://a.test/", "http://sub.b.test/", "http://c.test/", "http://outgoing.test/", "http://unrelated.test/"}
				d.Proxy = ProxySettings{Mode: "http", URL: proxy.URL}
				no := false
				d.Network.RespectRobots = &no
				d.Termination.WhenDrained = true
				d.RequestDelay = "0s"
				record := createWorkspaceCrawl(t, workspace, d)
				startWorkspaceCrawl(t, workspace, record)
				id := record["id"].(string)
				detail := waitWorkspace(t, workspace, id, func(v map[string]interface{}) bool { return latestRun(v)["state"] == "completed" })
				ids = append(ids, id)
				runs = append(runs, latestRun(detail)["id"].(string))
			}
			ledger, e := report.Open(filepath.Join(workspace.runRoot(ids[0], runs[0]), "state.json.report.sqlite"), false)
			require.NoError(t, e)
			require.NoError(t, ledger.Discover(context.Background(), []report.URL{
				{URI: "http://bad.test/", Origin: "http://a.test/"},
				{URI: "http://a.test/", Origin: "http://sub.b.test/"},
				{URI: "http://sub.b.test/", Origin: "http://c.test/"},
				{URI: "http://c.test/", Origin: "http://bad.test/"}, // cycle
				{URI: "http://outgoing.test/", Origin: "http://bad.test/"},
			}))
			ledger.Close()
			// A different crawl's incoming graph must not expand the target set.
			ledger, e = report.Open(filepath.Join(workspace.runRoot(ids[1], runs[1]), "state.json.report.sqlite"), false)
			require.NoError(t, e)
			require.NoError(t, ledger.Discover(context.Background(), []report.URL{{URI: "http://bad.test/", Origin: "http://unrelated.test/"}}))
			ledger.Close()
			incident, e := workspace.protection.Trigger("http://bad.test/", "synthetic", ids[0], runs[0], depth)
			require.ErrorIs(t, e, protection.ErrBlocked)
			require.Eventually(t, func() bool {
				i, e := workspace.protection.PrivateIncident(incident.ID)
				return e == nil && (i.State == "completed" || i.State == "failed")
			}, 10*time.Second, 20*time.Millisecond)
			incident, e = workspace.protection.PrivateIncident(incident.ID)
			require.NoError(t, e)
			require.Equal(t, "completed", incident.State, incident.Error)
			require.Len(t, incident.Targets, depth+1)
			expected := []string{"bad.test", "a.test", "b.test", "c.test"}
			for n, site := range expected {
				if n <= depth {
					require.ErrorIs(t, workspace.protection.Check("http://"+site+"/"), protection.ErrBlocked)
				} else {
					require.NoError(t, workspace.protection.Check("http://"+site+"/"))
				}
			}
			for n, id := range ids {
				raw, e := os.ReadFile(filepath.Join(workspace.runRoot(id, runs[n]), "results.jsonl"))
				require.NoError(t, e)
				for _, site := range expected[:depth+1] {
					require.NotContains(t, string(raw), site)
				}
				require.Contains(t, string(raw), "outgoing.test")
				require.Contains(t, string(raw), "unrelated.test")
			}
			require.NoError(t, workspace.Close())
			workspace, e = NewWorkspace(context.Background(), options)
			require.NoError(t, e)
			restored, e := workspace.protection.PrivateIncident(incident.ID)
			require.NoError(t, e)
			require.Equal(t, incident.Targets, restored.Targets)
			require.Equal(t, "completed", restored.State)
			require.NoError(t, workspace.protection.Check("http://unrelated.test/"))
		})
	}
}

func TestPurgeTargetCaptureSurvivesGraphRemovalAndRestart(t *testing.T) {
	options := Options{DataDirectory: t.TempDir()}
	workspace, e := NewWorkspace(context.Background(), options)
	require.NoError(t, e)
	defer func() { workspace.Close() }()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://bad.test/", "http://ref.test/", "http://good.test/"}
	record := createWorkspaceCrawl(t, workspace, d)
	id := record["id"].(string)
	// Persist a run without making any destination requests.
	workspace.mu.Lock()
	r := workspace.records[id]
	run := CrawlRun{ID: "synthetic-run", Definition: d, State: "stopped", Desired: "stopped"}
	r.Runs = append(r.Runs, run)
	require.NoError(t, workspace.saveLocked())
	workspace.mu.Unlock()
	path := filepath.Join(workspace.runRoot(id, run.ID), "state.json.report.sqlite")
	ledger, e := report.Open(path, false)
	require.NoError(t, e)
	require.NoError(t, ledger.Discover(context.Background(), []report.URL{{URI: "http://bad.test/", Origin: "http://ref.test/"}}))
	ledger.Close()
	incident, e := workspace.protection.Trigger("http://bad.test/", "synthetic", id, run.ID, 1)
	require.ErrorIs(t, e, protection.ErrBlocked)
	// This is the durable propagation boundary. Subsequent cleanup must not
	// recalculate the target set from a partially deleted historical graph.
	incident.Targets = []protection.Target{{Site: "bad.test", Distance: 0}, {Site: "ref.test", Distance: 1}}
	incident.Progress["targets"] = "captured"
	incident.Revision = 1
	require.NoError(t, workspace.protection.SetIncident(incident))
	require.NoError(t, os.Remove(path))
	require.NoError(t, workspace.Close())
	workspace, e = NewWorkspace(context.Background(), options)
	require.NoError(t, e)
	require.Eventually(t, func() bool {
		i, e := workspace.protection.PrivateIncident(incident.ID)
		return e == nil && (i.State == "completed" || i.State == "failed")
	}, 10*time.Second, 20*time.Millisecond)
	actual, e := workspace.protection.PrivateIncident(incident.ID)
	require.NoError(t, e)
	require.Equal(t, "completed", actual.State, actual.Error)
	require.Equal(t, incident.Targets, actual.Targets)
	require.NoError(t, workspace.protection.Check("http://good.test/"))
}

func TestPurgeFailureIsVisibleAndRetryUsesCapturedTargets(t *testing.T) {
	workspace, e := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	require.NoError(t, e)
	defer workspace.Close()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://bad.test/", "http://ref.test/", "http://good.test/"}
	record := createWorkspaceCrawl(t, workspace, d)
	id := record["id"].(string)
	run := CrawlRun{ID: "fault-run", Definition: d, State: "stopped", Desired: "stopped"}
	workspace.mu.Lock()
	r := workspace.records[id]
	r.Runs = append(r.Runs, run)
	require.NoError(t, workspace.saveLocked())
	workspace.mu.Unlock()
	root := workspace.runRoot(id, run.ID)
	require.NoError(t, os.MkdirAll(root, 0700))
	path := filepath.Join(root, "state.json.report.sqlite")
	ledger, e := report.Open(path, false)
	require.NoError(t, e)
	require.NoError(t, ledger.Discover(context.Background(), []report.URL{{URI: "http://bad.test/", Origin: "http://ref.test/"}}))
	ledger.Close()
	results := filepath.Join(root, "results.jsonl")
	raw := "{\"uri\":\"http://bad.test/\"}\n{\"uri\":\"http://good.test/\"}\ninvalid synthetic record\n"
	require.NoError(t, os.WriteFile(results, []byte(raw), 0600))
	incident, e := workspace.protection.Trigger("http://bad.test/", "synthetic", id, run.ID, 1)
	require.ErrorIs(t, e, protection.ErrBlocked)
	require.Eventually(t, func() bool {
		i, e := workspace.protection.PrivateIncident(incident.ID)
		return e == nil && i.State == "failed"
	}, 10*time.Second, 20*time.Millisecond)
	failed, e := workspace.protection.PrivateIncident(incident.ID)
	require.NoError(t, e)
	require.NotEmpty(t, failed.Error)
	require.Equal(t, "captured", failed.Progress["targets"])
	require.Len(t, failed.Targets, 2)
	before, e := os.ReadFile(results)
	require.NoError(t, e)
	require.Equal(t, raw, string(before), "failed rewrite changed the original")
	// Repair the controlled fault and change the historical graph. Retry may
	// finish cleanup, but must not expand its already-authorized target set.
	require.NoError(t, os.WriteFile(results, []byte(strings.ReplaceAll(raw, "invalid synthetic record\n", "")), 0600))
	ledger, e = report.Open(path, false)
	require.NoError(t, e)
	require.NoError(t, ledger.Discover(context.Background(), []report.URL{{URI: "http://bad.test/", Origin: "http://later-referrer.test/"}}))
	ledger.Close()
	request, _ := json.Marshal(map[string]interface{}{"action": "retry", "id": incident.ID})
	_, e = workspace.HandleWorkspace(context.Background(), "protection", request)
	require.NoError(t, e)
	require.Eventually(t, func() bool {
		i, e := workspace.protection.PrivateIncident(incident.ID)
		return e == nil && (i.State == "completed" || i.State == "failed")
	}, 10*time.Second, 20*time.Millisecond)
	complete, e := workspace.protection.PrivateIncident(incident.ID)
	require.NoError(t, e)
	require.Equal(t, "completed", complete.State, complete.Error)
	require.Equal(t, failed.Targets, complete.Targets)
	require.NoError(t, workspace.protection.Check("http://later-referrer.test/"))
	after, e := os.ReadFile(results)
	require.NoError(t, e)
	require.NotContains(t, string(after), "bad.test")
	require.Contains(t, string(after), "good.test")
}

func TestHistoricalProtectionCleanupPreservesUnknownSettingsAndIntegers(t *testing.T) {
	raw := json.RawMessage(`{"definition":{"seeds":["http://bad.test/","http://good.test/"],"obsolete_interval":9007199254740993},"pipeline":{"config":{"http_client":{"http_proxy_url":"http://operator:secret@bad.test:8080","unknown_integer":9007199254740995},"obsolete_controller":"keep"},"workers":[{"type":"fixed_seed_publisher","parameters":{"uris":["http://bad.test/","http://good.test/"],"unknown_integer":9007199254740997}}]}}`)
	match := protection.NewMatcher([]protection.Entry{{Domain: true, Value: "bad.test"}}, nil).Matches
	clean, e := purgeConfigurationJSON(raw, match)
	require.NoError(t, e)
	require.Contains(t, string(clean), `"obsolete_interval":9007199254740993`)
	require.Contains(t, string(clean), `"unknown_integer":9007199254740995`)
	require.Contains(t, string(clean), `"unknown_integer":9007199254740997`)
	require.Contains(t, string(clean), `"obsolete_controller":"keep"`)
	require.Contains(t, string(clean), "http://operator:secret@bad.test:8080")
	require.NotContains(t, string(clean), `"http://bad.test/"`)
	require.Contains(t, string(clean), "http://good.test/")
}

func TestProtectionActivationRetryIsIdempotentAcrossRestart(t *testing.T) {
	options := Options{DataDirectory: t.TempDir()}
	workspace, e := NewWorkspace(context.Background(), options)
	require.NoError(t, e)
	defer func() { workspace.Close() }()
	call := func(action string, fields map[string]interface{}) (interface{}, error) {
		fields["action"] = action
		raw, _ := json.Marshal(fields)
		return workspace.HandleWorkspace(context.Background(), "protection", raw)
	}
	preview, e := call("preview", map[string]interface{}{"revision": 1, "list": map[string]interface{}{"name": "idempotent", "text": "idempotent.test", "refresh": "6h", "enabled": true}})
	require.NoError(t, e)
	token := preview.(map[string]interface{})["token"]
	_, e = call("apply", map[string]interface{}{"revision": 1, "token": token, "confirmed": true})
	require.NoError(t, e)
	initial := workspace.protection.PublicSnapshot()
	require.Len(t, initial.Incidents, 1)
	_, e = call("apply", map[string]interface{}{"revision": 1, "token": token, "confirmed": true})
	require.NoError(t, e)
	require.Equal(t, initial.Revision, workspace.protection.PublicSnapshot().Revision)
	require.NoError(t, workspace.Close())
	workspace, e = NewWorkspace(context.Background(), options)
	require.NoError(t, e)
	_, e = call("apply", map[string]interface{}{"revision": 1, "token": token, "confirmed": true})
	require.NoError(t, e)
	actual := workspace.protection.PublicSnapshot()
	require.Len(t, actual.Lists, 1)
	require.Len(t, actual.Incidents, 1)
	require.Equal(t, initial.Incidents[0].ID, actual.Incidents[0].ID)
}
