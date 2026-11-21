package application

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/report"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Report requests use their own compact endpoints, never the full status/source
// response. Existing run ownership and storage paths remain authoritative.
func (w *Workspace) handleReport(ctx context.Context, r *CrawlRecord, slot *crawlSlot, action string, raw json.RawMessage) (interface{}, error) {
	var req struct {
		RunID string `json:"run_id"`
		Query string `json:"query"`
		After string `json:"after"`
		Limit int    `json:"limit"`
		report.GraphOptions
	}
	req.GraphOptions.Depth = 2
	if e := json.Unmarshal(raw, &req); e != nil {
		return nil, e
	}
	slot.mu.Lock()
	w.mu.Lock()
	if w.records[r.ID] != r || r.Deletion != nil {
		w.mu.Unlock()
		slot.mu.Unlock()
		return nil, fmt.Errorf("crawl unavailable")
	}
	var run CrawlRun
	found := false
	for _, candidate := range r.Runs {
		if req.RunID == "" || candidate.ID == req.RunID {
			run = candidate
			found = true
		}
	}
	w.mu.Unlock()
	if !found {
		slot.mu.Unlock()
		return nil, fmt.Errorf("run required")
	}
	a, e := prepareRunPipeline(w.runRoot(r.ID, run.ID), run)
	if e != nil {
		slot.mu.Unlock()
		return nil, e
	}
	path := a.Config.API.OperationsPath + ".report.sqlite"
	_, statErr := os.Stat(path)
	store, e := report.Open(path, os.IsNotExist(statErr))
	if e != nil {
		slot.mu.Unlock()
		return nil, e
	}
	// Existing legacy data is imported once in a background job. It never holds
	// the crawl slot during a whole scan or invents missing historical links.
	counts, e := store.Counts(ctx)
	if e == nil && counts.Partial {
		done, _ := store.Backfilled(ctx)
		if !done {
			w.mu.Lock()
			if w.reportImports == nil {
				w.reportImports = map[string]bool{}
			}
			if !w.reportImports[path] && !w.closing {
				w.reportImports[path] = true
				w.background.Add(1)
				go w.backfillReport(r.ID, run.ID, path, a.Config.API.OperationsPath, slot)
			}
			w.mu.Unlock()
		}
	}
	supervisor := slot.supervisor
	slot.mu.Unlock()
	defer store.Close()
	if e != nil {
		return nil, e
	}
	if action == "hosts" {
		return store.Search(ctx, req.Query, req.After, req.Limit)
	}
	if action == "host_graph" {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		return store.Graph(ctx, req.GraphOptions)
	}
	rate := 0.0
	basis := "60s"
	state := run.State
	if supervisor != nil {
		supervisor.mu.Lock()
		if supervisor.control != nil && run.ID == supervisor.runID {
			frame := supervisor.control.snapshot()
			state = supervisor.status
			if frame.Fetch != nil && state == "running" {
				rate = frame.Fetch.Rates["attempts"]
			}
		}
		supervisor.mu.Unlock()
	}
	if state == "stopped" || run.State == "completed" || run.State == "failed" {
		basis = "average"
		if run.Elapsed > 0 && run.Summary != nil && run.Summary.Fetch != nil {
			rate = float64(run.Summary.Fetch.Totals.Attempts) / (float64(run.Elapsed) / float64(time.Second))
		}
	}
	backfilled, _ := store.Backfilled(ctx)
	backfillError, _ := store.Progress(ctx, "error")
	return map[string]interface{}{"run_id": run.ID, "state": state, "requests_per_second": rate, "rate_basis": basis, "counts": counts, "backfill_pending": counts.Partial && !backfilled, "backfill_error": backfillError}, nil
}
func (w *Workspace) backfillReport(crawlID, runID, path, statePath string, slot *crawlSlot) {
	defer w.background.Done()
	defer func() { w.mu.Lock(); delete(w.reportImports, path); w.mu.Unlock() }()
	// Open while deletion/startup ownership is held; no path is recreated after a
	// concurrent deletion has accepted the crawl.
	slot.mu.Lock()
	w.mu.Lock()
	r := w.records[crawlID]
	valid := r != nil && r.Deletion == nil
	w.mu.Unlock()
	if !valid {
		slot.mu.Unlock()
		return
	}
	s, e := report.Open(path, true)
	slot.mu.Unlock()
	if e != nil {
		return
	}
	defer s.Close()
	w.mu.Lock()
	var run CrawlRun
	for _, candidate := range r.Runs {
		if candidate.ID == runID {
			run = candidate
		}
	}
	w.mu.Unlock()
	a, e := prepareRunPipeline(w.runRoot(crawlID, runID), run)
	if e != nil {
		return
	}
	// Pages are idempotent, so an interrupted import safely replays on restart.
	for _, resource := range a.Resources {
		if resource.Type != "frontier_store" {
			continue
		}
		var p struct {
			Path string `json:"path"`
		}
		json.Unmarshal(resource.Parameters, &p)
		checkpointKey := "frontier:" + resource.Name
		cursor, _ := s.Progress(w.ctx, checkpointKey)
		if cursor == "done" {
			continue
		}
		for {
			if w.ctx.Err() != nil || !w.reportImportActive(crawlID) {
				return
			}
			slot.mu.Lock()
			var records []report.URL
			var next string
			var live interface {
				ReportPage(context.Context, string, int) ([]report.URL, string, error)
			}
			if slot.supervisor != nil {
				slot.supervisor.mu.Lock()
				control := slot.supervisor.control
				if slot.supervisor.runID != runID {
					control = nil
				}
				slot.supervisor.mu.Unlock()
				if control != nil {
					control.mu.Lock()
					if control.prepared != nil {
						live, _ = control.prepared.resources[resource.Name].(interface {
							ReportPage(context.Context, string, int) ([]report.URL, string, error)
						})
					}
					control.mu.Unlock()
				}
			}
			if live != nil {
				records, next, e = live.ReportPage(w.ctx, cursor, 2048)
			} else {
				records, next, e = frontier.ReadReportPage(w.ctx, p.Path, cursor, 2048)
			}
			slot.mu.Unlock()
			if e != nil {
				break
			}
			if e = s.Discover(w.ctx, records); e != nil {
				return
			}
			if next == "" {
				s.Checkpoint(w.ctx, checkpointKey, "done")
				break
			}
			if e = s.Checkpoint(w.ctx, checkpointKey, next); e != nil {
				return
			}
			cursor = next
		}
	}
	for _, worker := range a.Workers {
		if worker.Type != "jsonl_sink" {
			continue
		}
		var params struct {
			Path string `json:"path"`
		}
		json.Unmarshal(worker.Parameters, &params)
		file, e := os.Open(filepath.Clean(params.Path))
		if e != nil {
			continue
		}
		key := "jsonl:" + worker.Name
		offsetText, _ := s.Progress(w.ctx, key)
		offset, _ := strconv.ParseInt(offsetText, 10, 64)
		if _, e = file.Seek(offset, 0); e != nil {
			file.Close()
			return
		}
		scan := bufio.NewScanner(file)
		scan.Buffer(make([]byte, 64*1024), 32*1024*1024)
		scan.Split(func(data []byte, atEOF bool) (int, []byte, error) {
			if i := bytes.IndexByte(data, '\n'); i >= 0 {
				return i + 1, data[:i], nil
			}
			if atEOF && len(data) > 0 {
				return len(data), data, nil
			}
			return 0, nil, nil
		})
		events := []report.Event{}
		batchLines := 0
		flush := func() error {
			if e := s.Import(w.ctx, events); e != nil {
				return e
			}
			events = nil
			batchLines = 0
			return s.Checkpoint(w.ctx, key, strconv.FormatInt(offset, 10))
		}
		for scan.Scan() {
			if w.ctx.Err() != nil || !w.reportImportActive(crawlID) {
				file.Close()
				return
			}
			offset += int64(len(scan.Bytes()) + 1)
			batchLines++
			var envelope types.Message
			if json.Unmarshal(scan.Bytes(), &envelope) == nil && envelope.MessageType == types.CompositeAnalysisType {
				var c message.CompositeAnalysis
				if json.Unmarshal(envelope.Message, &c) == nil {
					events = append(events, report.Event{URL: report.URL{URI: c.URI, Origin: c.Origin}, Status: c.HTTPCode})
					base := c.URI
					if c.EffectiveURI != "" {
						base = c.EffectiveURI
						if c.HTTPCode > 0 && base != c.URI {
							events = append(events, report.Event{URL: report.URL{URI: base, Origin: c.URI}, Status: c.HTTPCode})
						}
					}
					var links []string
					if c.LoadPermissive("url", &links) {
						for _, link := range links {
							uri, err := frontier.Normalize(link, base)
							if err == nil && reportLinkAllowed(run.Definition, c, uri) {
								events = append(events, report.Event{URL: report.URL{URI: uri, Origin: base}})
							}
							if len(events) >= 2048 {
								if e = s.Import(w.ctx, events); e != nil {
									file.Close()
									return
								}
								events = nil
							}
						}
					}
				}
			}
			if batchLines >= 128 {
				if e = flush(); e != nil {
					file.Close()
					return
				}
			}
		}
		if e = flush(); e != nil {
			file.Close()
			return
		}

		e = scan.Err()
		file.Close()
		if e != nil {
			return
		}
	}
	w.backfillIndexed(s, r, run)
	s.FinishBackfill(w.ctx)
}

func (w *Workspace) reportImportActive(crawlID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	r := w.records[crawlID]
	return r != nil && r.Deletion == nil && !w.closing
}
