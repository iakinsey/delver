package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/application"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"net/http"
	"os"
	"path/filepath"
)

// Synthetic saved history exercises the real resumable analysis operation.
func repeatedHistoryFixture(root string, workspace *application.Workspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CrawlID string `json:"crawl_id"`
			Count   int    `json:"count"`
		}
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil || req.Count < 1 || req.Count > 20000 {
			http.Error(w, "invalid fixture", 400)
			return
		}
		raw, _ := json.Marshal(map[string]string{"crawl_id": req.CrawlID})
		v, err := workspace.HandleCrawls(r.Context(), "detail", raw)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		raw, _ = json.Marshal(v)
		var record struct {
			ID   string `json:"id"`
			Runs []struct {
				ID    string `json:"id"`
				State string `json:"state"`
			} `json:"runs"`
		}
		if err = json.Unmarshal(raw, &record); err != nil || len(record.Runs) == 0 {
			http.Error(w, "run required", 400)
			return
		}
		run := record.Runs[len(record.Runs)-1]
		if run.State != "completed" && run.State != "stopped" {
			http.Error(w, "settled run required", 400)
			return
		}
		path := filepath.Join(root, "crawls", record.ID, "runs", run.ID, "results.jsonl")
		file, err := os.Open(path)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		reader := bufio.NewScanner(file)
		reader.Buffer(make([]byte, 64<<10), 8<<20)
		var envelope types.Message
		var c message.CompositeAnalysis
		for reader.Scan() {
			if json.Unmarshal(reader.Bytes(), &envelope) == nil && json.Unmarshal(envelope.Message, &c) == nil && c.Success && c.Has("text") {
				break
			}
		}
		err = reader.Err()
		file.Close()
		if err != nil || c.URI == "" {
			http.Error(w, "analysis unavailable", 500)
			return
		}
		file, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer file.Close()
		writer := bufio.NewWriter(file)
		for i := 0; i < req.Count; i++ {
			if err := r.Context().Err(); err != nil {
				return
			}
			envelope.ID = fmt.Sprintf("stored-replay-%d", i)
			c.RequestID = types.UUID(envelope.ID)
			c.Timestamp += 1
			c.QueuedAt += 1
			envelope.Message, _ = json.Marshal(c)
			line, _ := json.Marshal(envelope)
			if _, err = writer.Write(append(line, '\n')); err != nil {
				break
			}
		}
		if err == nil {
			err = writer.Flush()
		}
		if err == nil {
			err = file.Sync()
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(204)
	}
}
