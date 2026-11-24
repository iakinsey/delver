package application

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/report"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"io"
	"os"
	"syscall"
	"time"
)

type RepeatedAnalysisProgress struct {
	ReadComplete     bool   `json:"read_complete"`
	RoutingTarget    int64  `json:"routing_target"`
	ProjectionTarget int64  `json:"projection_target"`
	RunID            string `json:"run_id"`
	Epoch            int64  `json:"epoch"`
	Offset           int64  `json:"offset"`
	Limit            int64  `json:"limit"`
	File             string `json:"file"`
	Records          int64  `json:"records"`
	Oversized        int64  `json:"oversized"`
	Unavailable      int64  `json:"unavailable"`
	Protected        int64  `json:"protected"`
	Complete         bool   `json:"complete"`
}

func resultPath(run CrawlRun, root string) (string, error) {
	a, err := prepareRunPipeline(root, run)
	if err != nil {
		return "", err
	}
	for _, worker := range a.Workers {
		if worker.Type == "jsonl_sink" {
			var p struct {
				Path string `json:"path"`
			}
			if err = json.Unmarshal(worker.Parameters, &p); err != nil {
				return "", err
			}
			return p.Path, nil
		}
	}
	return "", fmt.Errorf("stored successful analyses unavailable: no supported result sink")
}
func boundedAnalysisLine(ctx context.Context, r *bufio.Reader) ([]byte, int64, error) {
	const maximum = 8 << 20
	var line []byte
	var n int64
	oversized := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, n, err
		}
		part, err := r.ReadSlice('\n')
		n += int64(len(part))
		if !oversized {
			if len(line)+len(part) > maximum {
				line = nil
				oversized = true
			} else {
				line = append(line, part...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if oversized {
			return nil, n, err
		}
		return line, n, err
	}
}
func (w *Workspace) executeRepeatedAnalysis(parent context.Context, r *CrawlRecord, slot *crawlSlot, op *Operation) error {
	var req repeatedRequest
	if err := json.Unmarshal(op.Input, &req); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	w.mu.Lock()
	if w.repeatedJobs == nil {
		w.repeatedJobs = map[string]context.CancelFunc{}
	}
	progress := RepeatedAnalysisProgress{Epoch: time.Now().UnixNano(), RunID: req.RunID}
	if req.Resume != "" {
		previous := r.Operations[req.Resume]
		if previous == nil {
			for _, candidate := range r.Operations {
				if candidate.ID == req.Resume {
					previous = candidate
					break
				}
			}
		}
		if previous == nil || previous.Analysis == nil {
			w.mu.Unlock()
			return fmt.Errorf("analysis checkpoint unavailable")
		}
		progress = *previous.Analysis
	}
	if progress.RunID == "" && len(r.Runs) > 0 {
		progress.RunID = r.Runs[len(r.Runs)-1].ID
	}
	initial := progress
	op.Analysis = &initial
	if err := w.saveLocked(); err != nil {
		w.mu.Unlock()
		return err
	}
	w.repeatedJobs[r.ID] = cancel
	if op.CancelRequested {
		cancel()
	}
	w.mu.Unlock()
	defer func() { w.mu.Lock(); delete(w.repeatedJobs, r.ID); w.mu.Unlock() }()
	for !progress.Complete {
		if err := ctx.Err(); err != nil {
			return err
		}
		slot.mu.Lock()
		err := w.withRepeatedStore(ctx, r, slot, progress.RunID, func(store *frontier.Store, ledger *report.Store, run CrawlRun) error {
			progress.RunID = run.ID
			if w.protection.HasUnfinished() {
				return fmt.Errorf("analysis interrupted by protection cleanup; resume after cleanup")
			}
			var err error
			if !progress.ReadComplete {
				path, err := resultPath(run, w.runRoot(r.ID, run.ID))
				if err != nil {
					return err
				}
				file, err := os.Open(path)
				if err != nil {
					return fmt.Errorf("stored analyses unavailable: %w", err)
				}
				defer file.Close()
				stat, err := file.Stat()
				if err != nil {
					return err
				}
				if !stat.Mode().IsRegular() {
					return fmt.Errorf("results are not a regular file")
				}
				identity := fmt.Sprintf("%d", stat.ModTime().UnixNano())
				if st, ok := stat.Sys().(*syscall.Stat_t); ok {
					identity = fmt.Sprintf("%d:%d", st.Dev, st.Ino)
				}
				if progress.File != identity || stat.Size() < progress.Offset {
					progress.File = identity
					progress.Offset = 0
					progress.Limit = stat.Size()
				}
				if _, err = file.Seek(progress.Offset, io.SeekStart); err != nil {
					return err
				}
				reader := bufio.NewReaderSize(io.LimitReader(file, progress.Limit-progress.Offset), 64<<10)
				for n := 0; n < 64; n++ {
					if err := ctx.Err(); err != nil {
						return err
					}
					if w.protection.HasUnfinished() {
						return fmt.Errorf("analysis interrupted by protection cleanup; resume after cleanup")
					}
					line, size, readErr := boundedAnalysisLine(ctx, reader)
					if size == 0 && readErr == io.EOF {
						progress.ReadComplete = true
						break
					}
					// A live writer may have an incomplete last record. Leave its offset for
					// a later explicit pass rather than treating partial JSON as an analysis.
					if readErr == io.EOF && size > 0 && len(line) > 0 && line[len(line)-1] != '\n' {
						progress.Unavailable++
						progress.ReadComplete = true
						break
					}
					if readErr != nil && readErr != io.EOF {
						return readErr
					}
					if line == nil {
						progress.Oversized++
						progress.Offset += size
						continue
					}
					var envelope types.Message
					var c message.CompositeAnalysis
					if json.Unmarshal(bytes.TrimSpace(line), &envelope) != nil || envelope.MessageType != types.CompositeAnalysisType || json.Unmarshal(envelope.Message, &c) != nil {
						progress.Unavailable++
						progress.Offset += size
						continue
					}
					if _, err := frontier.Normalize(c.URI, ""); err != nil {
						progress.Unavailable++
						progress.Offset += size
						continue
					}
					if !c.Success {
						progress.Offset += size
						continue
					}
					if err = w.protection.CheckPayload(envelope.Message); errors.Is(err, protection.ErrBlocked) {
						progress.Protected++
						progress.Offset += size
						continue
					} else if err != nil {
						return err
					}
					var links features.URIs
					c.LoadPermissive(features.UrlField, &links)
					id := envelope.ID
					if id == "" {
						id = fmt.Sprintf("%x", sha256.Sum256(line))
					}
					if err = store.AnalyzeRepeated(ctx, id, c, links, progress.Epoch); errors.Is(err, protection.ErrBlocked) {
						progress.Protected++
					} else if err != nil {
						return err
					} else {
						progress.Records++
					}
					progress.Offset += size
					if readErr == io.EOF {
						progress.ReadComplete = true
						break
					}
				}
			}

			if err = store.MaintainRepeatedRouting(ctx, 128); err != nil {
				return err
			}
			state, err := store.RepeatedPages(ctx, "", "", 1)
			if err != nil {
				return err
			}
			if progress.ReadComplete && progress.RoutingTarget == 0 {
				progress.RoutingTarget = state.Routing.Target
				progress.ProjectionTarget = state.Revision
			}
			if err = store.FlushRepeated(ctx, ledger); err != nil {
				return err
			}
			projected, err := ledger.RepeatedRevision(ctx)
			if err != nil {
				return err
			}
			progress.Complete = progress.ReadComplete && state.Routing.Completed >= progress.RoutingTarget && projected >= progress.ProjectionTarget
			return nil
		})
		slot.mu.Unlock()
		if err != nil {
			return err
		}
		w.mu.Lock()
		copy := progress
		op.Analysis = &copy
		op.Updated = time.Now().Unix()
		err = w.saveLocked()
		w.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}
