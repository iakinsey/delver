package application

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/flow"
	"github.com/stretchr/testify/require"
)

func finishWorkspaceOperation(t *testing.T, w *Workspace, id, action string, payload map[string]interface{}) map[string]interface{} {
	t.Helper()
	payload["crawl_id"] = id
	op := workspaceCall(t, w, action, payload).(map[string]interface{})
	return waitWorkspace(t, w, id, func(v map[string]interface{}) bool {
		for _, raw := range v["operations"].(map[string]interface{}) {
			entry := raw.(map[string]interface{})
			if entry["id"] != op["id"] {
				continue
			}
			require.NotEqual(t, "failed", entry["state"], "operation failed: %v", entry)
			return entry["state"] == "completed"
		}
		return false
	})
}

func TestWorkspaceConfigurationRetainsRunState(t *testing.T) {
	RegisterResource("reconfigure_metadata", func(json.RawMessage) (interface{}, error) { return struct{}{}, nil }, ProtectionMetadataOnly)
	defer RegisterResource("reconfigure_metadata", nil)
	for _, tc := range []struct {
		state, action  string
		expert, reopen bool
	}{
		{"paused", "resume", false, false}, {"stopped", "start", false, false},
		{"paused", "start_run", false, false}, {"running", "activate", false, false},
		{"paused", "resume", true, false}, {"stopped", "start", true, false},
		{"running", "start_run", true, false}, {"stopped", "activate", true, false},
		{"paused", "resume", false, true}, {"stopped", "start", true, true},
	} {
		t.Run(fmt.Sprintf("%s/%s/expert=%v/reopen=%v", tc.state, tc.action, tc.expert, tc.reopen), func(t *testing.T) {
			var mu sync.Mutex
			visits := map[string]int{}
			agents := map[string]int{}
			site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				visits[r.URL.Path]++
				agents[r.UserAgent()]++
				mu.Unlock()
				w.Header().Set("Content-Type", "text/html")
				if r.URL.Path == "/" {
					for i := 0; i < 120; i++ {
						fmt.Fprintf(w, `<a href="/page/%d">page</a>`, i)
					}
				} else {
					fmt.Fprint(w, "<title>page</title>")
				}
			}))
			defer site.Close()
			options := Options{DataDirectory: t.TempDir()}
			w, err := NewWorkspace(context.Background(), options)
			require.NoError(t, err)
			defer func() { w.Close() }()
			d := DefaultCrawlDefinition()
			d.Seeds = []string{site.URL + "/"}
			d.Scope = "same_origin"
			d.MaxDepth = 1
			d.Revisit = "24h"
			d.RequestDelay = "50ms"
			d.Concurrency = 1
			respectRobots := false
			d.Network.RespectRobots = &respectRobots
			d.Network.UserAgent = "before-config"
			d.Fetch.Mode = "fixed"
			d.Fetch.Concurrency = 1
			record := createWorkspaceCrawl(t, w, d)
			id := record["id"].(string)
			finishWorkspaceOperation(t, w, id, "start_run", map[string]interface{}{"revision": record["revision"]})
			waitWorkspace(t, w, id, func(v map[string]interface{}) bool {
				rt := v["runtime"].(map[string]interface{})["runtime"].(map[string]interface{})
				// Discovery and result persistence are independent worker paths.
				// Establish both prerequisites before testing their preservation.
				runID := latestRun(v)["id"].(string)
				info, err := os.Stat(filepath.Join(w.runRoot(id, runID), "results.jsonl"))
				return rt["frontier"].(map[string]interface{})["pending_and_active"].(float64) > 80 && err == nil && info.Size() > 0
			})
			action := "pause"
			if tc.state == "stopped" {
				action = "stop"
			}
			// Capture a quiescent checkpoint even for a running-edit case.
			before := finishWorkspaceOperation(t, w, id, "command", map[string]interface{}{"action": action})
			if tc.state == "running" {
				before = finishWorkspaceOperation(t, w, id, "command", map[string]interface{}{"action": "resume"})
			}
			run := latestRun(before)
			runID := run["id"].(string)
			created := run["created"]
			elapsed := run["elapsed_ns"].(float64)
			snapshot, _ := json.Marshal(run["configuration_snapshot"])
			root := w.runRoot(id, runID)
			results, err := os.ReadFile(filepath.Join(root, "results.jsonl"))
			require.NoError(t, err)
			require.NotEmpty(t, results)
			var totals flow.Totals
			if summary, ok := run["summary"].(map[string]interface{}); ok {
				raw, _ := json.Marshal(summary["fetch"].(map[string]interface{})["totals"])
				require.NoError(t, json.Unmarshal(raw, &totals))
			}
			d.Concurrency = 4
			d.Seeds = append(d.Seeds, site.URL+"/added")
			d.Fetch.Concurrency = 4
			d.Network.UserAgent = "after-config"
			d.Strategy = "breadth_first"
			request := map[string]interface{}{"revision": before["revision"], "definition": d}
			if tc.expert {
				app, err := CompileCrawl(filepath.Join(w.root, "preview"), "preview", d)
				require.NoError(t, err)
				app.Workers[len(app.Workers)-1].Name = "custom_cleanup"
				// Preserve an opaque integer without float conversion in custom JSON.
				app.Resources = append(app.Resources, config.Resource{Name: "custom_metadata", Type: "reconfigure_metadata", Parameters: json.RawMessage(`{"opaque":9007199254740993}`)})
				request["configuration"] = app
			}
			draft := workspaceCall(t, w, "draft", withCrawlID(request, id)).(map[string]interface{})
			if tc.reopen {
				require.NoError(t, w.Close())
				w, err = NewWorkspace(context.Background(), options)
				require.NoError(t, err)
			}
			payload := map[string]interface{}{"revision": draft["revision"], "id": "apply-once"}
			endpoint := tc.action
			if tc.action == "start" || tc.action == "resume" {
				endpoint = "command"
				payload["action"] = tc.action
			}
			after := finishWorkspaceOperation(t, w, id, endpoint, payload)
			require.Len(t, after["runs"].([]interface{}), 1)
			current := latestRun(after)
			require.Equal(t, runID, current["id"])
			require.Equal(t, created, current["created"])
			require.GreaterOrEqual(t, current["elapsed_ns"].(float64), elapsed)
			currentSnapshot, _ := json.Marshal(current["configuration_snapshot"])
			require.JSONEq(t, string(snapshot), string(currentSnapshot))
			require.NotContains(t, after, "draft")
			require.NotContains(t, after, "expert_draft")
			if !tc.expert {
				require.Equal(t, float64(4), current["definition"].(map[string]interface{})["concurrency"])
			}
			waitWorkspace(t, w, id, func(v map[string]interface{}) bool {
				mu.Lock()
				defer mu.Unlock()
				return agents["after-config"] > 0 && visits["/added"] > 0
			})
			stopped := finishWorkspaceOperation(t, w, id, "command", map[string]interface{}{"action": "stop"})
			stored, err := os.ReadFile(filepath.Join(root, "results.jsonl"))
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(string(stored), string(results)), "results were overwritten")
			var final flow.Totals
			raw, _ := json.Marshal(latestRun(stopped)["summary"].(map[string]interface{})["fetch"].(map[string]interface{})["totals"])
			require.NoError(t, json.Unmarshal(raw, &final))
			require.GreaterOrEqual(t, final.Attempts, totals.Attempts)
			require.GreaterOrEqual(t, final.Successes, totals.Successes)
			mu.Lock()
			require.Equal(t, 1, visits["/"], "completed seed was fetched again")
			mu.Unlock()
			entries, err := os.ReadDir(filepath.Join(w.root, "crawls", id, "runs"))
			require.NoError(t, err)
			require.Len(t, entries, 1)
			if tc.expert {
				pipeline := workspaceCall(t, w, "pipeline", map[string]interface{}{"crawl_id": id}).(map[string]interface{})
				text := pipeline["configuration_json"].(string)
				require.Contains(t, text, "9007199254740993")
				require.Contains(t, text, "custom_cleanup")
				var compiled config.Application
				require.NoError(t, json.Unmarshal([]byte(text), &compiled))
				require.Equal(t, 4, compiled.Config.HTTPClient.PerOriginConcurrency)
			}
			// Retrying an accepted operation after another command must not apply
			// configuration twice or start the stopped runtime again.
			again := finishWorkspaceOperation(t, w, id, endpoint, payload)
			require.Equal(t, "stopped", runtimeState(again))
			require.Len(t, again["runs"].([]interface{}), 1)
		})
	}
}

func withCrawlID(payload map[string]interface{}, id string) map[string]interface{} {
	payload["crawl_id"] = id
	return payload
}

func TestWorkspaceCompletedBudgetCanBeRaisedInSameRun(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<a href="/next">next</a>`)
	}))
	defer site.Close()
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	require.NoError(t, err)
	defer w.Close()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{site.URL}
	respectRobots := false
	d.Network.RespectRobots = &respectRobots
	d.Termination.MaxRequests = 1
	record := createWorkspaceCrawl(t, w, d)
	id := record["id"].(string)
	finishWorkspaceOperation(t, w, id, "start_run", map[string]interface{}{"revision": record["revision"]})
	before := waitWorkspace(t, w, id, func(v map[string]interface{}) bool { return latestRun(v)["state"] == "completed" })
	runID := latestRun(before)["id"]
	d.Termination.MaxRequests = 2
	draft := workspaceCall(t, w, "draft", map[string]interface{}{"crawl_id": id, "revision": before["revision"], "definition": d}).(map[string]interface{})
	finishWorkspaceOperation(t, w, id, "command", map[string]interface{}{"action": "start", "revision": draft["revision"]})
	after := waitWorkspace(t, w, id, func(v map[string]interface{}) bool { return latestRun(v)["state"] == "completed" })
	require.Len(t, after["runs"].([]interface{}), 1)
	require.Equal(t, runID, latestRun(after)["id"])
	summary := latestRun(after)["summary"].(map[string]interface{})
	require.Equal(t, float64(2), summary["frontier"].(map[string]interface{})["dispatched"])
}

func TestWorkspaceRejectsStorageReplacementBeforeStopping(t *testing.T) {
	for _, field := range []string{"path", "crawl_id"} {
		t.Run(field, func(t *testing.T) {
			site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, "<title>retained</title>")
			}))
			defer site.Close()
			w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
			require.NoError(t, err)
			defer w.Close()
			d := DefaultCrawlDefinition()
			d.Seeds = []string{site.URL}
			d.Network.RespectRobots = boolPointer(false)
			record := createWorkspaceCrawl(t, w, d)
			id := record["id"].(string)
			app, err := CompileCrawl(filepath.Join(w.root, "preview"), "preview", d)
			require.NoError(t, err)
			draft := workspaceCall(t, w, "draft", map[string]interface{}{"crawl_id": id, "revision": record["revision"], "configuration": app}).(map[string]interface{})
			finishWorkspaceOperation(t, w, id, "activate", map[string]interface{}{"revision": draft["revision"]})
			waitWorkspace(t, w, id, func(v map[string]interface{}) bool {
				return v["runtime"].(map[string]interface{})["runtime"].(map[string]interface{})["fetch"].(map[string]interface{})["totals"].(map[string]interface{})["successes"].(float64) > 0
			})
			before := finishWorkspaceOperation(t, w, id, "command", map[string]interface{}{"action": "pause"})
			runID := latestRun(before)["id"]
			// Keep the existing custom parameters and change only its storage binding.
			w.mu.Lock()
			raw, err := json.Marshal(w.records[id].Expert)
			w.mu.Unlock()
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(raw, &app))
			for i, res := range app.Resources {
				if res.Name == "requests" {
					var p map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(res.Parameters, &p))
					value := "another-namespace"
					if field == "path" {
						value = filepath.Join(w.runRoot(id, runID.(string)), "another-queue")
					}
					p[field], err = json.Marshal(value)
					require.NoError(t, err)
					app.Resources[i].Parameters, err = json.Marshal(p)
					require.NoError(t, err)
				}
			}
			draft = workspaceCall(t, w, "draft", map[string]interface{}{"crawl_id": id, "revision": before["revision"], "configuration": app}).(map[string]interface{})
			op := workspaceCall(t, w, "activate", map[string]interface{}{"crawl_id": id, "revision": draft["revision"], "id": "reject-binding"}).(map[string]interface{})
			after := waitWorkspace(t, w, id, func(v map[string]interface{}) bool {
				for _, raw := range v["operations"].(map[string]interface{}) {
					entry := raw.(map[string]interface{})
					if entry["id"] == op["id"] {
						if entry["state"] == "failed" {
							require.Contains(t, entry["error"], "storage identity")
							return true
						}
					}
				}
				return false
			})
			require.Equal(t, "paused", runtimeState(after))
			require.Equal(t, runID, latestRun(after)["id"])
			require.Len(t, after["runs"].([]interface{}), 1)
			require.Contains(t, after, "expert_draft")
		})
	}
}

func TestWorkspaceConfigurationStartupFailureCanBeRepairedInSameRun(t *testing.T) {
	RegisterResource("reconfigure_failure", func(json.RawMessage) (interface{}, error) {
		return nil, fmt.Errorf("injected configuration startup failure")
	}, ProtectionMetadataOnly)
	defer RegisterResource("reconfigure_failure", nil)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<title>retained</title>")
	}))
	defer site.Close()
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	require.NoError(t, err)
	defer w.Close()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{site.URL}
	d.Network.RespectRobots = boolPointer(false)
	record := createWorkspaceCrawl(t, w, d)
	id := record["id"].(string)
	finishWorkspaceOperation(t, w, id, "start_run", map[string]interface{}{"revision": record["revision"]})
	waitWorkspace(t, w, id, func(v map[string]interface{}) bool {
		return v["runtime"].(map[string]interface{})["runtime"].(map[string]interface{})["fetch"].(map[string]interface{})["totals"].(map[string]interface{})["successes"].(float64) > 0
	})
	before := finishWorkspaceOperation(t, w, id, "command", map[string]interface{}{"action": "stop"})
	run := latestRun(before)
	root := w.runRoot(id, run["id"].(string))
	results, err := os.ReadFile(filepath.Join(root, "results.jsonl"))
	require.NoError(t, err)
	app, err := CompileCrawl(root, run["id"].(string), d)
	require.NoError(t, err)
	app.Resources = append(app.Resources, config.Resource{Name: "broken", Type: "reconfigure_failure", Parameters: json.RawMessage(`{}`)})
	draft := workspaceCall(t, w, "draft", map[string]interface{}{"crawl_id": id, "revision": before["revision"], "configuration": app}).(map[string]interface{})
	op := workspaceCall(t, w, "activate", map[string]interface{}{"crawl_id": id, "revision": draft["revision"], "id": "fail-startup"}).(map[string]interface{})
	failed := waitWorkspace(t, w, id, func(v map[string]interface{}) bool {
		for _, raw := range v["operations"].(map[string]interface{}) {
			entry := raw.(map[string]interface{})
			if entry["id"] == op["id"] && entry["state"] == "failed" {
				return true
			}
		}
		return false
	})
	require.Equal(t, run["id"], latestRun(failed)["id"])
	require.Len(t, failed["runs"].([]interface{}), 1)
	app.Resources = app.Resources[:len(app.Resources)-1]
	draft = workspaceCall(t, w, "draft", map[string]interface{}{"crawl_id": id, "revision": failed["revision"], "configuration": app}).(map[string]interface{})
	after := finishWorkspaceOperation(t, w, id, "command", map[string]interface{}{"action": "resume", "revision": draft["revision"]})
	require.Equal(t, "running", runtimeState(after))
	require.Equal(t, run["id"], latestRun(after)["id"])
	stored, err := os.ReadFile(filepath.Join(root, "results.jsonl"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(stored), string(results)))
}

func TestGuidedHostConcurrencyBounds(t *testing.T) {
	for _, count := range []int{0, 1, 2, 4, 8192, 8193} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			d := DefaultCrawlDefinition()
			d.Seeds = []string{"http://example.test"}
			d.Concurrency = count
			_, err := CompileCrawl(t.TempDir(), "test", d)
			if count < 1 || count > 8192 {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRunStorageAllowsOpaqueCustomParameters(t *testing.T) {
	for _, parameters := range []json.RawMessage{json.RawMessage(`9007199254740993`), json.RawMessage(`[9007199254740993]`), json.RawMessage(`{"opaque":9007199254740993}`)} {
		app := config.Application{Resources: []config.Resource{{Name: "custom", Type: "opaque", Parameters: parameters}}}
		require.NoError(t, validateExistingStorage(app, app))
	}
}
