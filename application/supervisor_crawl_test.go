package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSupervisedCrawlPauseRecrawlAndRestart(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			fmt.Fprint(w, "User-agent: *\nAllow: /\n")
			return
		}
		fmt.Fprint(w, "<title>Supervised</title>")
	}))
	defer site.Close()
	data, err := os.ReadFile("testdata/examples/minimal.json")
	require.NoError(t, err)
	root := t.TempDir()
	raw := strings.ReplaceAll(string(data), "run-data/minimal", root)
	raw = strings.ReplaceAll(raw, "http://localhost:8088/", site.URL+"/")
	raw = strings.ReplaceAll(raw, `"localhost"`, `"127.0.0.1"`)
	app, err := Decode(strings.NewReader(raw))
	require.NoError(t, err)
	app.Config.API.OperationsPath = filepath.Join(root, "ops.json")
	s, err := NewSupervisor(context.Background(), app)
	require.NoError(t, err)
	defer s.Close()
	require.Equal(t, "completed", runCommand(t, s, "start", "start").State)
	output := filepath.Join(root, "results.jsonl")
	waitLines := func(min int) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			data, _ := os.ReadFile(output)
			if strings.Count(string(data), "\n") >= min {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("crawl output did not arrive")
	}
	waitLines(1)
	entries := s.log.snapshot()
	require.NotEmpty(t, entries)
	require.Equal(t, 200, entries[0].Status)
	require.Equal(t, "completed", runCommand(t, s, "pause", "pause").State)
	s.mu.Lock()
	require.Equal(t, "paused", s.status)
	s.mu.Unlock()
	data, err = json.Marshal(map[string]string{"id": "recrawl", "action": "recrawl", "queue": "requests", "record": site.URL + "/"})
	require.NoError(t, err)
	require.NoError(t, s.Execute(context.Background(), "recrawl", "requests", site.URL+"/"))
	require.NoError(t, s.Close())
	s, err = NewSupervisor(context.Background(), app)
	s.state.Desired = "paused"
	require.NoError(t, err)
	defer s.Close()
	s.mu.Lock()
	require.NoError(t, s.start())
	require.Equal(t, "paused", s.status)
	s.mu.Unlock()
	snapshot := s.control.snapshot()
	require.EqualValues(t, 1, snapshot.Queues["requests"])
	before, _ := os.ReadFile(output)
	require.Equal(t, 1, strings.Count(string(before), "\n"))
	require.Equal(t, "completed", runCommand(t, s, "resume", "resume").State)
	waitLines(2)
	require.Equal(t, "completed", runCommand(t, s, "stop", "stop").State)
	require.Equal(t, "completed", runCommand(t, s, "restart", "start").State)
	require.Equal(t, "completed", runCommand(t, s, "stop-again", "stop").State)
}
