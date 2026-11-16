package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDocumentedExamples(t *testing.T) {
	repository, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	search := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/":
			fmt.Fprint(w, `{"version":{"number":"7.17.1"},"tagline":"You Know, for Search"}`)
		case r.URL.Path == "/_bulk":
			fmt.Fprint(w, `{"errors":false,"items":[]}`)
		default:
			fmt.Fprint(w, `{"acknowledged":true}`)
		}
	}))
	defer search.Close()
	var site *httptest.Server
	files := http.FileServer(http.Dir(filepath.Join(repository, "examples/site/pages")))
	site = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/feed.xml" {
			data, err := os.ReadFile(filepath.Join(repository, "examples/site/pages/feed.xml"))
			if err != nil {
				t.Error(err)
				http.Error(w, "fixture", 500)
				return
			}
			w.Header().Set("Content-Type", "application/rss+xml")
			fmt.Fprint(w, strings.ReplaceAll(string(data), "http://localhost:8088", site.URL))
			return
		}
		files.ServeHTTP(w, r)
	}))
	defer site.Close()
	for _, name := range []string{"minimal", "dfs", "news"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(repository, "application/testdata/examples", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Decode(bytes.NewReader(raw)); err != nil {
				t.Fatalf("documented config invalid: %v", err)
			}
			storage := t.TempDir()
			text := strings.ReplaceAll(string(raw), "run-data/"+name, storage)
			text = strings.ReplaceAll(text, "http://localhost:8088", site.URL)
			text = strings.ReplaceAll(text, "http://localhost:9200", search.URL)
			var object map[string]interface{}
			if err = json.Unmarshal([]byte(text), &object); err != nil {
				t.Fatal(err)
			}
			conf := object["config"].(map[string]interface{})
			for _, key := range []string{"countries_path", "companies_path"} {
				if relative, ok := conf[key].(string); ok {
					conf[key] = filepath.Join(repository, relative)
				}
			}
			conf["api"] = map[string]interface{}{"enabled": false}
			conf["streamer"] = map[string]interface{}{"enabled": false}
			host, _ := url.Parse(site.URL)
			conf["http_client"].(map[string]interface{})["allowed_domains"] = []string{host.Hostname()}
			conf["http_client"].(map[string]interface{})["request_delay"] = "0s"
			data, _ := json.Marshal(object)
			app, err := Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- Run(ctx, app) }()
			defer cancel()
			wanted := 3
			if name == "news" {
				wanted = 1
			}
			deadline := time.Now().Add(45 * time.Second)
			count := 0
			for time.Now().Before(deadline) {
				data, _ := os.ReadFile(filepath.Join(storage, "results.jsonl"))
				count = bytes.Count(data, []byte("\n"))
				if count >= wanted {
					break
				}
				select {
				case err := <-done:
					t.Fatalf("example exited: %v", err)
				case <-time.After(25 * time.Millisecond):
				}
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("example shutdown timed out")
			}
			if count < wanted {
				t.Fatalf("example produced %d results, wanted %d", count, wanted)
			}
		})
	}
}
