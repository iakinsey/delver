package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/types/message"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLocalCrawl(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{}
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/robots.txt":
			fmt.Fprint(w, "User-agent: *\nDisallow: /denied\n")
		case "/":
			fmt.Fprint(w, `<title>Home</title><a href="/child">child</a><a href="/child#duplicate">same</a><a href="/redirect">redirect</a><a href="/denied">denied</a>`)
		case "/child":
			fmt.Fprint(w, `<title>Child</title><a href="/">home</a>`)
		case "/redirect":
			http.Redirect(w, r, "/dir/page", http.StatusFound)
		case "/dir/page":
			fmt.Fprint(w, `<title>Redirected</title><a href="next">next</a>`)
		case "/dir/next":
			fmt.Fprint(w, `<title>Next</title>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer site.Close()
	root := t.TempDir()
	output := filepath.Join(root, "results.jsonl")
	fileQueue := func(name string) map[string]interface{} {
		return map[string]interface{}{"name": name, "type": "file_queue", "parameters": map[string]interface{}{"name": name, "path": filepath.Join(root, name), "dlq_path": filepath.Join(root, name+"-dlq"), "reset": true, "max_poll_delay_ms": 1, "max_attempts": 3}}
	}
	raw := map[string]interface{}{"config": map[string]interface{}{"workers": map[string]interface{}{"worker_counts": 2}, "api": map[string]interface{}{"enabled": false}, "streamer": map[string]interface{}{"enabled": false}, "metrics": map[string]interface{}{"enabled": false}, "http_client": map[string]interface{}{"request_delay": "0s"}, "shutdown_timeout": "5s"}, "resources": []interface{}{map[string]interface{}{"name": "requests", "type": "crawl_queue", "parameters": map[string]interface{}{"path": filepath.Join(root, "crawl"), "crawl_id": "integration", "max_attempts": 3}}, fileQueue("fetched"), fileQueue("analyzed"), fileQueue("results"), map[string]interface{}{"name": "objects", "type": "filesystem_object_store", "parameters": map[string]interface{}{"path": filepath.Join(root, "objects")}}, map[string]interface{}{"name": "urls", "type": "multi_host_map", "parameters": map[string]interface{}{"base_path": filepath.Join(root, "urls")}}}, "workers": []interface{}{map[string]interface{}{"name": "seeds", "type": "fixed_seed_publisher", "manager": "job", "interval": "1h", "outbox": "requests", "parameters": map[string]interface{}{"uris": []string{site.URL + "/"}}}, map[string]interface{}{"name": "fetch", "type": "http_fetcher", "inbox": "requests", "outbox": "fetched", "parameters": map[string]interface{}{"object_store": "objects"}}, map[string]interface{}{"name": "extract", "type": "composite_extractor", "inbox": "fetched", "outbox": "analyzed", "parameters": map[string]interface{}{"object_store": "objects", "transformer_queue": "results", "enabled": []string{"url", "text", "title"}}}, map[string]interface{}{"name": "discover", "type": "dfs_basic_accumulator", "inbox": "analyzed", "outbox": "requests", "parameters": map[string]interface{}{"url_store": "urls", "max_depth": 2}}, map[string]interface{}{"name": "save", "type": "jsonl_sink", "inbox": "results", "parameters": map[string]interface{}{"path": output}}}}
	data, _ := json.Marshal(raw)
	app, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, app) }()
	defer cancel()
	deadline := time.Now().Add(10 * time.Second)
	var results []message.CompositeAnalysis
	for time.Now().Before(deadline) {
		content, _ := os.ReadFile(output)
		results = nil
		for _, line := range bytes.Split(bytes.TrimSpace(content), []byte("\n")) {
			var envelope struct {
				Message message.CompositeAnalysis `json:"message"`
			}
			if json.Unmarshal(line, &envelope) == nil {
				results = append(results, envelope.Message)
			}
		}
		if len(results) >= 5 {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("application exited: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("shutdown blocked")
	}
	if len(results) != 5 {
		t.Fatalf("want five analyses, got %d", len(results))
	}
	mu.Lock()
	defer mu.Unlock()
	if hits["/denied"] != 0 {
		t.Fatal("robots-disallowed page fetched")
	}
	if hits["/child"] != 1 || hits["/"] != 1 || hits["/dir/next"] != 1 {
		t.Fatalf("deduplication or redirect resolution failed: %v", hits)
	}
	for _, result := range results {
		if result.URI == site.URL+"/redirect" && result.EffectiveURI != site.URL+"/dir/page" {
			t.Fatal("effective URI lost")
		}
	}
}
func TestStrictConfiguration(t *testing.T) {
	for _, raw := range []string{`{"config":{"http_client":{"max_retires":3}}}`, `{"workers":[{"name":"bad","type":"http_fetcher","inbox":"missing"}]}`, `{"config":{"shutdown_timeout":"invalid"}}`} {
		if _, err := Decode(strings.NewReader(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
