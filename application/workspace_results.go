package application

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"io"
	"os"
	"path/filepath"
)

// RecentResults reads a bounded tail, so polling a long-running crawl never
// scans its full history. Full records remain in the configured result sink.
func (w *Workspace) recentResults(r *CrawlRecord, runID string) (interface{}, error) {
	w.mu.Lock()
	var run CrawlRun
	found := false
	for _, candidate := range r.Runs {
		if runID == "" || candidate.ID == runID {
			run = candidate
			found = true
		}
	}
	id := r.ID
	w.mu.Unlock()
	if !found {
		return map[string]interface{}{"rows": []interface{}{}, "available": false}, nil
	}
	root := w.runRoot(id, run.ID)
	a, err := prepareRunPipeline(root, run)
	if err != nil {
		return nil, err
	}
	path := ""
	for _, worker := range a.Workers {
		if worker.Type == "jsonl_sink" {
			var p struct {
				Path string `json:"path"`
			}
			if json.Unmarshal(worker.Parameters, &p) == nil {
				path = p.Path
				break
			}
		}
	}
	if path == "" {
		return map[string]interface{}{"rows": []interface{}{}, "available": false, "run_id": run.ID}, nil
	}
	file, err := os.Open(filepath.Clean(path))
	if os.IsNotExist(err) {
		return map[string]interface{}{"rows": []interface{}{}, "available": true, "run_id": run.ID}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("result preview unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	const maximum = 4 << 20
	offset := int64(0)
	if info.Size() > maximum {
		offset = info.Size() - maximum
	}
	data := make([]byte, info.Size()-offset)
	n, err := file.ReadAt(data, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	data = data[:n]
	lines := bytes.Split(data, []byte{'\n'})
	if offset > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	if len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}
	rows := []map[string]interface{}{}
	for i := len(lines) - 1; i >= 0 && len(rows) < 50; i-- {
		var envelope types.Message
		if json.Unmarshal(lines[i], &envelope) != nil {
			continue
		}
		if w.protection.CheckPayload(envelope.Message) != nil {
			continue
		}
		var c message.CompositeAnalysis
		if envelope.MessageType != types.CompositeAnalysisType || json.Unmarshal(envelope.Message, &c) != nil {
			continue
		}
		title := ""
		c.LoadPermissive("title", &title)
		if len(title) > 500 {
			title = title[:500]
		}
		errorText := c.Error
		if len(errorText) > 1000 {
			errorText = errorText[:1000]
		}
		rows = append(rows, map[string]interface{}{"uri": c.URI, "title": sanitizeText(title, a, configurationForSecrets(run.Definition)), "http_status": c.HTTPCode, "error": sanitizeText(errorText, a, configurationForSecrets(run.Definition)), "timestamp": c.Timestamp})
	}
	return publicValue(map[string]interface{}{"rows": rows, "available": true, "run_id": run.ID, "limited": offset > 0}), nil
}
