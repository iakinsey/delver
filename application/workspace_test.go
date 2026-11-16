package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func workspaceCall(t *testing.T, w *Workspace, action string, p interface{}) interface{} {
	t.Helper()
	b, _ := json.Marshal(p)
	v, err := w.HandleCrawls(context.Background(), action, b)
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
	b, _ = json.Marshal(v)
	var out interface{}
	json.Unmarshal(b, &out)
	return out
}
func createWorkspaceCrawl(t *testing.T, w *Workspace, d CrawlDefinition) map[string]interface{} {
	return workspaceCall(t, w, "create", map[string]interface{}{"definition": d}).(map[string]interface{})
}
func startWorkspaceCrawl(t *testing.T, w *Workspace, r map[string]interface{}) {
	workspaceCall(t, w, "start_run", map[string]interface{}{"crawl_id": r["id"], "revision": r["revision"]})
	waitWorkspace(t, w, r["id"].(string), func(v map[string]interface{}) bool {
		runs := v["runs"].([]interface{})
		return len(runs) > 0 && runtimeState(v) == "running"
	})
}
func waitWorkspace(t *testing.T, w *Workspace, id string, predicate func(map[string]interface{}) bool) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var last map[string]interface{}
	for time.Now().Before(deadline) {
		last = workspaceCall(t, w, "detail", map[string]interface{}{"crawl_id": id}).(map[string]interface{})
		if predicate(last) {
			return last
		}
		time.Sleep(20 * time.Millisecond)
	}
	b, _ := json.Marshal(last)
	t.Fatalf("crawl did not reach expected state: %s", b)
	return nil
}
func runtimeState(v map[string]interface{}) string {
	runtime, _ := v["runtime"].(map[string]interface{})
	s, _ := runtime["state"].(string)
	return s
}
func latestRun(v map[string]interface{}) map[string]interface{} {
	runs := v["runs"].([]interface{})
	return runs[len(runs)-1].(map[string]interface{})
}
func TestWorkspaceConcurrentCrawlsAndIndependentLifecycle(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{}
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			fmt.Fprint(w, "User-agent: *\nAllow: /\n")
			return
		}
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<title>%s</title><a href="%s/child">Child</a>`, r.URL.Path, strings.TrimSuffix(r.URL.Path, "/child"))
	}))
	defer site.Close()
	options := Options{DataDirectory: t.TempDir()}
	w, err := NewWorkspace(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	d := DefaultCrawlDefinition()
	d.Name = "First"
	d.Seeds = []string{site.URL + "/first"}
	d.Scope = "same_origin"
	d.Revisit = "800ms"
	d.MaxDepth = 1
	first := createWorkspaceCrawl(t, w, d)
	d.Name = "Second"
	d.Seeds = []string{site.URL + "/second"}
	d.RequestDelay = "200ms"
	d.Strategy = "depth_first"
	second := createWorkspaceCrawl(t, w, d)
	startWorkspaceCrawl(t, w, first)
	startWorkspaceCrawl(t, w, second)
	id1, id2 := first["id"].(string), second["id"].(string)
	waitWorkspace(t, w, id1, func(v map[string]interface{}) bool { return runtimeState(v) == "running" })
	waitWorkspace(t, w, id2, func(v map[string]interface{}) bool { return runtimeState(v) == "running" })
	workspaceCall(t, w, "command", map[string]string{"crawl_id": id1, "id": "pause-first", "action": "pause"})
	waitWorkspace(t, w, id1, func(v map[string]interface{}) bool { return runtimeState(v) == "paused" })
	mu.Lock()
	before := hits["/second"]
	mu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	advanced := false
	for time.Now().Before(deadline) {
		mu.Lock()
		advanced = hits["/second"] > before
		mu.Unlock()
		if advanced {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !advanced {
		t.Fatal("second crawl stopped progressing while first was paused")
	}
	firstView := workspaceCall(t, w, "detail", map[string]string{"crawl_id": id1}).(map[string]interface{})
	if runtimeState(firstView) != "paused" {
		t.Fatal(firstView)
	}
	secondView := workspaceCall(t, w, "detail", map[string]string{"crawl_id": id2}).(map[string]interface{})
	if secondView["definition"].(map[string]interface{})["request_delay"] != "200ms" {
		t.Fatal(secondView)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w, err = NewWorkspace(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitWorkspace(t, w, id1, func(v map[string]interface{}) bool { return runtimeState(v) == "paused" })
	waitWorkspace(t, w, id2, func(v map[string]interface{}) bool { return runtimeState(v) == "running" })
	workspaceCall(t, w, "command", map[string]string{"crawl_id": id1, "id": "resume-first", "action": "resume"})
	waitWorkspace(t, w, id1, func(v map[string]interface{}) bool { return runtimeState(v) == "running" })
}
func TestWorkspaceDrainCompletionBudgetsAndRestart(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			fmt.Fprint(w, "User-agent: *\nAllow: /\n")
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<title>Root</title><a href="/one">One</a><a href="/two">Two</a>`)
	}))
	defer site.Close()
	options := Options{DataDirectory: t.TempDir()}
	w, err := NewWorkspace(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for _, tc := range []struct {
		name string
		term Termination
	}{{"drain", Termination{WhenDrained: true}}, {"budget", Termination{MaxRequests: 1}}, {"duration", Termination{Duration: "300ms"}}} {
		t.Run(tc.name, func(t *testing.T) {
			d := DefaultCrawlDefinition()
			d.Name = tc.name
			d.Seeds = []string{site.URL + "/"}
			d.Termination = tc.term
			r := createWorkspaceCrawl(t, w, d)
			startWorkspaceCrawl(t, w, r)
			id := r["id"].(string)
			v := waitWorkspace(t, w, id, func(v map[string]interface{}) bool { return latestRun(v)["state"] == "completed" })
			if runtimeState(v) != "stopped" {
				t.Fatal(v)
			}
			if tc.name == "budget" {
				b, _ := os.ReadFile(filepath.Join(options.DataDirectory, "crawls", id, "runs", latestRun(v)["id"].(string), "results.jsonl"))
				if strings.Count(string(b), "\n") != 1 {
					t.Fatalf("request cap violated: %s", b)
				}
			}
		})
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w, err = NewWorkspace(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	list := workspaceCall(t, w, "list", nil).([]interface{})
	for _, value := range list {
		if runtimeState(value.(map[string]interface{})) != "stopped" {
			t.Fatal(value)
		}
	}
}
func TestWorkspaceDraftConflictsAndProxySecrets(t *testing.T) {
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	d := DefaultCrawlDefinition()
	d.Name = "Proxy"
	d.Seeds = []string{"http://example.test/"}
	d.Proxy = ProxySettings{Mode: "http", URL: "http://operator:private-password@127.0.0.1:1234"}
	r := createWorkspaceCrawl(t, w, d)
	b, _ := json.Marshal(r)
	if !strings.Contains(string(b), "http://operator:private-password@127.0.0.1:1234") {
		t.Fatal("proxy address was hidden")
	}
	visible := r["definition"].(map[string]interface{})
	visible["name"] = "Renamed"
	saved := workspaceCall(t, w, "draft", map[string]interface{}{"crawl_id": r["id"], "revision": r["revision"], "definition": visible}).(map[string]interface{})
	w.mu.Lock()
	actual := w.records[r["id"].(string)].Draft.Proxy.URL
	w.mu.Unlock()
	if actual != d.Proxy.URL {
		t.Fatal("proxy address was not preserved")
	}
	raw, _ := json.Marshal(map[string]interface{}{"crawl_id": r["id"], "revision": r["revision"], "definition": visible})
	if _, err := w.HandleCrawls(context.Background(), "draft", raw); err == nil {
		t.Fatal("stale draft accepted")
	}
	if saved["revision"].(float64) <= r["revision"].(float64) {
		t.Fatal(saved)
	}
	copy := workspaceCall(t, w, "duplicate", map[string]interface{}{"crawl_id": r["id"]}).(map[string]interface{})
	if copy["id"] == r["id"] {
		t.Fatal("duplicate reused identity")
	}
}

func TestWorkspaceHTTPProxyAppliesToFeedsRobotsAndRedirects(t *testing.T) {
	var mu sync.Mutex
	paths := map[string]int{}
	authOK := true
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths[r.URL.Path]++
		if r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("operator:private-password")) {
			authOK = false
		}
		mu.Unlock()
		switch r.URL.Path {
		case "/robots.txt":
			fmt.Fprint(w, "User-agent: *\nAllow: /\n")
		case "/feed":
			w.Header().Set("Content-Type", "application/rss+xml")
			fmt.Fprint(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Feed</title><link>http://crawl-proxy.test/</link><description>Fixture</description><item><title>Article</title><link>http://crawl-proxy.test/article</link></item></channel></rss>`)
		case "/":
			http.Redirect(w, r, "http://crawl-proxy.test/home", http.StatusFound)
		default:
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<title>Proxied page</title>")
		}
	}))
	defer proxy.Close()
	options := Options{DataDirectory: t.TempDir()}
	w, err := NewWorkspace(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	d := DefaultCrawlDefinition()
	d.Name = "Proxied"
	d.Seeds = []string{"http://crawl-proxy.test/"}
	d.Feeds = []string{"http://crawl-proxy.test/feed"}
	d.Termination.WhenDrained = true
	d.Proxy = ProxySettings{Mode: "http", URL: strings.Replace(proxy.URL, "http://", "http://operator:private-password@", 1)}
	r := createWorkspaceCrawl(t, w, d)
	startWorkspaceCrawl(t, w, r)
	v := waitWorkspace(t, w, r["id"].(string), func(v map[string]interface{}) bool { return latestRun(v)["state"] == "completed" })
	mu.Lock()
	defer mu.Unlock()
	for _, path := range []string{"/robots.txt", "/feed", "/", "/home", "/article"} {
		if paths[path] == 0 {
			t.Fatalf("%s bypassed proxy: %v", path, paths)
		}
	}
	if !authOK {
		t.Fatal("proxy credentials not forwarded")
	}
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), d.Proxy.URL) {
		t.Fatal("proxy address was hidden in API")
	}
}

func TestWorkspaceExpertConversionPreservesSecretsAndCustomFields(t *testing.T) {
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	d := DefaultCrawlDefinition()
	d.Name = "Expert"
	d.Seeds = []string{"http://example.test/"}
	d.Proxy = ProxySettings{Mode: "http", URL: "http://operator:expert-secret@127.0.0.1:9999"}
	r := createWorkspaceCrawl(t, w, d)
	preview := workspaceCall(t, w, "pipeline", map[string]interface{}{"crawl_id": r["id"]}).(map[string]interface{})["configuration"].(map[string]interface{})
	workers := preview["workers"].([]interface{})
	workers[len(workers)-1].(map[string]interface{})["name"] = "custom_cleanup_name"
	workspaceCall(t, w, "draft", map[string]interface{}{"crawl_id": r["id"], "revision": r["revision"], "configuration": preview})
	w.mu.Lock()
	expert := w.records[r["id"].(string)].ExpertDraft
	w.mu.Unlock()
	if expert.Config.HTTPClient.HTTPProxyUrl != d.Proxy.URL {
		t.Fatal("expert conversion lost saved proxy")
	}
	if expert.Workers[len(expert.Workers)-1].Name != "custom_cleanup_name" {
		t.Fatal("expert worker settings lost")
	}
}
func TestWorkspaceWorkflowIdempotency(t *testing.T) {
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	d := DefaultCrawlDefinition()
	d.Name = "Idempotent"
	d.Seeds = []string{"http://127.0.0.1:1/"}
	r := createWorkspaceCrawl(t, w, d)
	payload := map[string]interface{}{"crawl_id": r["id"], "revision": r["revision"], "id": "same-workflow"}
	first := workspaceCall(t, w, "start_run", payload).(map[string]interface{})
	again := workspaceCall(t, w, "start_run", payload).(map[string]interface{})
	if first["id"] != again["id"] {
		t.Fatal("workflow identity changed")
	}
	waitWorkspace(t, w, r["id"].(string), func(v map[string]interface{}) bool { return len(v["runs"].([]interface{})) == 1 })
	third := workspaceCall(t, w, "start_run", payload).(map[string]interface{})
	if third["id"] != first["id"] {
		t.Fatal(third)
	}
}

func TestWorkspaceActiveClockExcludesPausedTime(t *testing.T) {
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	d := DefaultCrawlDefinition()
	d.Name = "Clock"
	d.Seeds = []string{"https://example.test/"}
	r := createWorkspaceCrawl(t, w, d)
	id := r["id"].(string)
	w.mu.Lock()
	w.records[id].Runs = []CrawlRun{{ID: "clock-run", Definition: d}}
	w.mu.Unlock()
	start := time.Now()
	w.onRunStatus(id, "clock-run", "running", start)
	w.onRunStatus(id, "clock-run", "pausing", start.Add(500*time.Millisecond))
	w.onRunStatus(id, "clock-run", "paused", start.Add(5*time.Second))
	w.onRunStatus(id, "clock-run", "running", start.Add(8*time.Second))
	w.onRunStatus(id, "clock-run", "stopping", start.Add(9*time.Second))
	w.mu.Lock()
	elapsed := w.records[id].Runs[0].Elapsed
	w.mu.Unlock()
	if time.Duration(elapsed) != 1500*time.Millisecond {
		t.Fatal(time.Duration(elapsed))
	}
}
func TestWorkspaceExpertTextPreservesLargeNumericParameters(t *testing.T) {
	RegisterResource("numeric_fixture", func(json.RawMessage) (interface{}, error) { return struct{}{}, nil })
	defer RegisterResource("numeric_fixture", nil)
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	d := DefaultCrawlDefinition()
	d.Name = "Numeric"
	d.Seeds = []string{"https://example.test/"}
	r := createWorkspaceCrawl(t, w, d)
	a, err := CompileCrawl(filepath.Join(t.TempDir(), "preview"), "preview", d)
	if err != nil {
		t.Fatal(err)
	}
	a.Resources = append(a.Resources, config.Resource{Name: "numeric", Type: "numeric_fixture", Parameters: json.RawMessage(`{"opaque_id":9007199254740993}`)})
	text, _ := json.Marshal(a)
	workspaceCall(t, w, "draft", map[string]interface{}{"crawl_id": r["id"], "revision": r["revision"], "configuration_json": string(text)})
	preview := workspaceCall(t, w, "pipeline", map[string]interface{}{"crawl_id": r["id"]}).(map[string]interface{})
	if !strings.Contains(preview["configuration_json"].(string), "9007199254740993") {
		t.Fatal("large custom numeric value was rounded")
	}
}
func TestWorkspaceCreationIdempotencyAndPersistenceFailure(t *testing.T) {
	root := t.TempDir()
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: root})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	d := DefaultCrawlDefinition()
	d.Name = "Once"
	d.Seeds = []string{"https://example.test/"}
	payload := map[string]interface{}{"id": "create-once", "definition": d}
	a := workspaceCall(t, w, "create", payload).(map[string]interface{})
	b := workspaceCall(t, w, "create", payload).(map[string]interface{})
	if a["id"] != b["id"] {
		t.Fatal("creation replay duplicated crawl")
	}
	if err := os.Mkdir(filepath.Join(root, "crawls.json.tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	d.Name = "Must not persist"
	raw, _ := json.Marshal(map[string]interface{}{"crawl_id": a["id"], "revision": a["revision"], "definition": d})
	if _, err := w.HandleCrawls(context.Background(), "draft", raw); err == nil {
		t.Fatal("expected storage failure")
	}
	w.mu.Lock()
	actual := w.records[a["id"].(string)]
	if actual.Draft != nil || actual.Revision != 1 {
		t.Error("failed draft changed active metadata")
	}
	w.mu.Unlock()
	if err := os.Remove(filepath.Join(root, "crawls.json.tmp")); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceLegacyImportPreservesSavedPipelineAndPausedState(t *testing.T) {
	root := t.TempDir()
	d := DefaultCrawlDefinition()
	d.Name = "Legacy"
	d.Seeds = []string{"https://example.test/"}
	legacy, err := CompileCrawl(filepath.Join(root, "legacy"), "legacy-run", d)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := newSupervisor(context.Background(), legacy)
	if err != nil {
		t.Fatal(err)
	}
	previous.mu.Lock()
	previous.state.Active.Config.HTTPClient.UserAgent = "saved-active-agent"
	previous.state.Desired = "paused"
	err = previous.save()
	previous.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := previous.Close(); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "legacy.json")
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	options := Options{DataDirectory: filepath.Join(root, "workspace"), ImportPath: file}
	w, err := NewWorkspace(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	values := workspaceCall(t, w, "list", nil).([]interface{})
	if len(values) != 1 {
		t.Fatal(values)
	}
	view := values[0].(map[string]interface{})
	if runtimeState(view) != "paused" {
		t.Fatal(view)
	}
	id := view["id"].(string)
	w.mu.Lock()
	expert := w.records[id].Expert
	w.mu.Unlock()
	if expert.Config.HTTPClient.UserAgent != "saved-active-agent" {
		t.Fatal("saved configuration was discarded")
	}
	var originalPath, importedPath struct {
		Path string `json:"path"`
	}
	json.Unmarshal(legacy.Resources[0].Parameters, &originalPath)
	json.Unmarshal(expert.Resources[0].Parameters, &importedPath)
	if originalPath.Path != importedPath.Path {
		t.Fatal("legacy crawl storage was relocated")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w, err = NewWorkspace(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if got := len(workspaceCall(t, w, "list", nil).([]interface{})); got != 1 {
		t.Fatal("repeat import duplicated crawl", got)
	}
}

func TestWorkspaceContinuesPreservedDiscoveriesAfterRequestLimit(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			fmt.Fprint(w, "User-agent: *\nAllow: /\n")
			return
		}
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/" {
			fmt.Fprint(w, `<title>Root</title><a href="/one">One</a><a href="/two">Two</a>`)
		} else {
			fmt.Fprintf(w, "<title>%s</title>", r.URL.Path)
		}
	}))
	defer site.Close()
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	d := DefaultCrawlDefinition()
	d.Name = "Continue"
	d.Seeds = []string{site.URL + "/"}
	d.Termination.MaxRequests = 1
	r := createWorkspaceCrawl(t, w, d)
	startWorkspaceCrawl(t, w, r)
	id := r["id"].(string)
	stopped := waitWorkspace(t, w, id, func(v map[string]interface{}) bool { return latestRun(v)["state"] == "completed" })
	originalRun := latestRun(stopped)["id"]
	summary := latestRun(stopped)["summary"].(map[string]interface{})["frontier"].(map[string]interface{})
	if summary["pending_and_active"].(float64) != 2 {
		t.Fatal(summary)
	}
	workspaceCall(t, w, "continue_run", map[string]interface{}{"crawl_id": id, "revision": stopped["revision"], "id": "continue-owned-work"})
	finished := waitWorkspace(t, w, id, func(v map[string]interface{}) bool {
		return latestRun(v)["continued"] == float64(1) && latestRun(v)["state"] == "completed"
	})
	if latestRun(finished)["id"] != originalRun {
		t.Fatal("continuation replaced the run instead of preserving its queue")
	}
	summary = latestRun(finished)["summary"].(map[string]interface{})["frontier"].(map[string]interface{})
	if summary["pending_and_active"].(float64) != 0 || summary["dispatched"].(float64) != 3 {
		t.Fatal(summary)
	}
}

func TestSupervisorOwnsConfigurationSlices(t *testing.T) {
	d := DefaultCrawlDefinition()
	d.Name = "Owned"
	d.Seeds = []string{"https://one.example/"}
	a, err := CompileCrawl(t.TempDir(), "owned-run", d)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(a)
	first, err := newSupervisor(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	first.mu.Lock()
	first.state.Active.Workers[0].Parameters = json.RawMessage(`{"uris":["https://two.example/"]}`)
	err = first.save()
	first.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	second, err := newSupervisor(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	after, _ := json.Marshal(a)
	if string(before) != string(after) {
		t.Fatal("restoring a supervisor mutated its caller's configuration")
	}
}
