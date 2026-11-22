package main

// Fault injection belongs only to the local browser fixture executable.
import (
	"bytes"
	"encoding/json"
	"github.com/iakinsey/delver/application"
	"net/http"
	"os"
	"path/filepath"
)

func protectionFault(root string, workspace *application.Workspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		var q struct {
			CrawlID string `json:"crawl_id"`
			Enabled bool   `json:"enabled"`
		}
		if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&q); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		raw, _ := json.Marshal(map[string]string{"crawl_id": q.CrawlID})
		value, e := workspace.HandleCrawls(r.Context(), "detail", raw)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		raw, _ = json.Marshal(value)
		var record struct {
			ID   string `json:"id"`
			Runs []struct {
				ID    string `json:"id"`
				State string `json:"state"`
			} `json:"runs"`
		}
		if e = json.Unmarshal(raw, &record); e != nil || len(record.Runs) == 0 {
			http.Error(w, "run required", 400)
			return
		}
		run := record.Runs[len(record.Runs)-1]
		if run.State != "completed" && run.State != "stopped" {
			http.Error(w, "settled run required", 400)
			return
		}
		path := filepath.Join(root, "crawls", record.ID, "runs", run.ID, "results.jsonl")
		raw, e = os.ReadFile(path)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		marker := []byte("synthetic invalid purge fixture\n")
		raw = bytes.ReplaceAll(raw, marker, nil)
		if q.Enabled {
			raw = append(raw, marker...)
		}
		if e = os.WriteFile(path, raw, 0600); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.WriteHeader(204)
	}
}
