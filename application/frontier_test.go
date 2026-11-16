package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/types/message"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFrontierExamples(t *testing.T) {
	for _, name := range []string{"frontier-site", "frontier-web", "frontier-adaptive", "frontier-permanent"} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load("testdata/examples/" + name + ".json"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFrontierSupervisedTraversal(t *testing.T) {
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
	data, err := os.ReadFile("testdata/examples/frontier-site.json")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	raw := strings.ReplaceAll(string(data), "run-data/frontier-site", root)
	raw = strings.ReplaceAll(raw, "http://localhost:8088/", site.URL+"/")
	raw = strings.ReplaceAll(raw, `"localhost"`, `"127.0.0.1"`)
	app, err := Decode(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range app.Resources {
		if r.Type == "frontier_store" {
			var p map[string]interface{}
			if err := json.Unmarshal(r.Parameters, &p); err != nil {
				t.Fatal(err)
			}
			p["max_pending"] = 1
			b, _ := json.Marshal(p)
			app.Resources[i].Parameters = b
		}
	}
	app.Config.API.Enabled = false
	app.Config.Streamer.Enabled = false
	s, err := NewSupervisor(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if op := runCommand(t, s, "start", "start"); op.State != "completed" {
		t.Fatal(op)
	}
	deadline := time.Now().Add(5 * time.Second)
	done := false
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(filepath.Join(root, "results.jsonl"))
		if strings.Count(string(b), "\n") >= 3 {
			done = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !done {
		t.Fatal("frontier did not traverse the site")
	}
	if op := runCommand(t, s, "pause-before-staging", "pause"); op.State != "completed" {
		t.Fatal(op)
	}
	s.control.mu.Lock()
	store := s.control.prepared.resources["frontier"].(frontier.FrontierStore)
	s.control.mu.Unlock()
	if err := store.StageDiscovery(context.Background(), "paused-discovery", []message.FetcherRequest{{URI: site.URL + "/three", Depth: 1}}); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"stop", "start"} {
		if op := runCommand(t, s, "staged-"+action, action); op.State != "completed" {
			t.Fatal(op)
		}
	}
	deadline = time.Now().Add(5 * time.Second)
	recovered := false
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(filepath.Join(root, "results.jsonl"))
		if strings.Count(string(b), "\n") >= 4 {
			recovered = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !recovered {
		t.Fatal("staged discovery did not resume after restart")
	}
	for _, action := range []string{"pause", "resume", "stop"} {
		if op := runCommand(t, s, fmt.Sprintf("%s-%d", action, time.Now().UnixNano()), action); op.State != "completed" {
			t.Fatal(op)
		}
	}
}
