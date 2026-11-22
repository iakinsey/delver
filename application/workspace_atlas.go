package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/report"
	"github.com/iakinsey/delver/types"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var atlasGeneration = make(chan struct{}, 1)

type atlasJob struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Phase  string `json:"phase"`
	Error  string `json:"error,omitempty"`
	cancel context.CancelFunc
	done   chan struct{}
}

func (w *Workspace) handleAtlas(ctx context.Context, path, action string, raw json.RawMessage) (result interface{}, resultErr error) {
	snapshotPath := path + ".atlas.sqlite"
	epoch := report.AtlasEpoch(snapshotPath)
	if action != "atlas_generate" && action != "atlas_cancel" && action != "atlas_status" {
		defer func() {
			if resultErr == nil && ((w.protection != nil && w.protection.HasUnfinished()) || epoch != report.AtlasEpoch(snapshotPath)) {
				result = nil
				resultErr = fmt.Errorf("graph invalidated by protection cleanup")
			}
		}()
	}
	if w.protection != nil && w.protection.HasUnfinished() {
		if action == "atlas_status" {
			return map[string]interface{}{"snapshot": nil, "job": nil, "protection_pending": true, "defaults": report.DefaultAtlasOptions()}, nil
		}
		return nil, fmt.Errorf("graph unavailable during protection cleanup")
	}
	path += ".atlas.sqlite"
	var req struct {
		Options *report.AtlasOptions `json:"options"`
		report.AtlasViewport
		HostID    int64   `json:"host_id"`
		Direction string  `json:"direction"`
		Offset    int     `json:"offset"`
		Query     string  `json:"query"`
		Z         int     `json:"z"`
		X         float64 `json:"x"`
		Y         float64 `json:"y"`
		Radius    float64 `json:"radius"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	switch action {
	case "atlas_generate":
		o := report.DefaultAtlasOptions()
		if req.Options != nil {
			o = *req.Options
		}
		if err := o.Validate(); err != nil {
			return nil, err
		}
		w.mu.Lock()
		if w.closing {
			w.mu.Unlock()
			return nil, fmt.Errorf("workspace is shutting down")
		}
		if w.atlasJobs == nil {
			w.atlasJobs = map[string]*atlasJob{}
		}
		if j := w.atlasJobs[path]; j != nil && j.State == "running" {
			w.mu.Unlock()
			return nil, fmt.Errorf("graph generation already running")
		}
		select {
		case atlasGeneration <- struct{}{}:
		default:
			w.mu.Unlock()
			return nil, fmt.Errorf("another graph is generating; retry after completion")
		}
		jobctx, cancel := context.WithTimeout(w.ctx, 30*time.Minute)
		j := &atlasJob{ID: string(types.NewV4()), State: "running", Phase: "Starting", cancel: cancel, done: make(chan struct{})}
		w.atlasJobs[path] = j
		w.background.Add(1)
		w.mu.Unlock()
		// Open a dedicated handle: the request handle closes when this RPC returns.
		sourcePath := strings.TrimSuffix(path, ".atlas.sqlite")
		_, statErr := os.Stat(sourcePath)
		source, err := report.Open(sourcePath, os.IsNotExist(statErr))
		if err != nil {
			cancel()
			<-atlasGeneration
			w.mu.Lock()
			delete(w.atlasJobs, path)
			w.mu.Unlock()
			close(j.done)
			w.background.Done()
			return nil, err
		}
		go func() {
			defer w.background.Done()
			defer close(j.done)
			defer cancel()
			defer source.Close()
			defer func() { <-atlasGeneration }()
			_, err := source.GenerateAtlas(jobctx, path, j.ID, o, func(phase string) { w.mu.Lock(); j.Phase = phase; w.mu.Unlock() })
			w.mu.Lock()
			defer w.mu.Unlock()
			j.State = "completed"
			j.Phase = "Ready"
			if err != nil {
				j.State = "failed"
				j.Phase = ""
				j.Error = err.Error()
				if jobctx.Err() != nil {
					j.State = "cancelled"
				}
			}
		}()
		return map[string]interface{}{"id": j.ID, "state": "running", "phase": "Starting"}, nil
	case "atlas_cancel":
		w.mu.Lock()
		defer w.mu.Unlock()
		if j := w.atlasJobs[path]; j != nil && j.State == "running" {
			j.cancel()
		}
		return map[string]bool{"cancelled": true}, nil
	case "atlas_status":
		out := map[string]interface{}{"defaults": report.DefaultAtlasOptions(), "job": nil, "snapshot": nil}
		w.mu.Lock()
		if j := w.atlasJobs[path]; j != nil {
			out["job"] = map[string]string{"id": j.ID, "state": j.State, "phase": j.Phase, "error": j.Error}
		}
		w.mu.Unlock()
		if _, err := os.Stat(path); err == nil {
			info, err := report.ReadAtlasInfo(ctx, path)
			if err != nil {
				return nil, err
			}
			out["snapshot"] = info
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		return out, nil
	case "atlas_tile":
		if req.X != math.Trunc(req.X) || req.Y != math.Trunc(req.Y) {
			return nil, fmt.Errorf("tile coordinates must be integers")
		}
		c, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		return report.AtlasTile(c, path, report.AtlasTileRequest{Snapshot: req.Snapshot, Z: req.Z, X: int(req.X), Y: int(req.Y), SelectedHost: req.SelectedHost})
	case "atlas_pick":
		return report.AtlasPick(ctx, path, req.Snapshot, req.X, req.Y, req.Radius)
	case "atlas_view":
		c, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		return report.AtlasView(c, path, req.AtlasViewport)
	case "atlas_host":
		return report.AtlasInspect(ctx, path, req.Snapshot, req.HostID, req.Direction, req.Offset)
	case "atlas_search":
		return report.AtlasSearch(ctx, path, req.Query, req.Snapshot)
	}
	return nil, fmt.Errorf("unknown graph action")
}

// Deletion waits for cancellation before removing a run directory, preventing
// an in-flight generator from recreating files after deletion.
func (w *Workspace) cancelAtlases(root string) {
	w.mu.Lock()
	jobs := []*atlasJob{}
	for path, j := range w.atlasJobs {
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) || strings.HasPrefix(path, root+".") {
			if j.State == "running" {
				j.cancel()
			}
			jobs = append(jobs, j)
			delete(w.atlasJobs, path)
		}
	}
	w.mu.Unlock()
	for _, j := range jobs {
		<-j.done
	}
}
