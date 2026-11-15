package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/api"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/gateway"
	"github.com/iakinsey/delver/types"
	ui "github.com/iakinsey/delver/web"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func (w *Workspace) importLegacy(a config.Application) error {
	if err := Validate(a); err != nil {
		return err
	}

	id := string(types.NewV4())
	runID := string(types.NewV4())
	d := DefaultCrawlDefinition()
	d.Name = "Imported crawl"
	if a.Config.API.OperationsPath == "" {
		a.Config.API.OperationsPath = a.Config.API.DashDBPath + ".operations.json"
	}
	sourceState := a.Config.API.OperationsPath
	if data, err := os.ReadFile(sourceState); err == nil {
		var stored operationsState
		if err := json.Unmarshal(data, &stored); err != nil {
			return err
		}
		if len(stored.Active.Workers) > 0 {
			a = stored.Active
			a.Config.API.OperationsPath = sourceState
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := Validate(a); err != nil {
		return err
	}
	if err := w.checkImportOwnership(a); err != nil {
		return err
	}
	for _, record := range w.records {
		for _, run := range record.Runs {
			if run.Expert != nil && run.Expert.Config.API.OperationsPath == a.Config.API.OperationsPath {
				a.Config.API.OperationsPath = filepath.Join(w.runRoot(id, runID), "state.json")
			}
		}
	}
	if a.Config.API.OperationsPath == ".operations.json" {
		a.Config.API.OperationsPath = filepath.Join(w.runRoot(id, runID), "state.json")
	}
	r := &CrawlRecord{ID: id, Revision: 1, Definition: d, Expert: &a, Runs: []CrawlRun{{ID: runID, Version: 1, Created: time.Now().Unix(), State: "running", Definition: d, Expert: &a, Legacy: true}}}
	if w.options.ImportPath != "" {
		r.LegacySource, _ = filepath.Abs(w.options.ImportPath)
	}
	w.records[id] = r
	return w.saveLocked()
}

// Handle preserves legacy single-crawl API calls while requiring selection when
// a workspace contains multiple crawls.
func (w *Workspace) Handle(ctx context.Context, action string, raw json.RawMessage) (interface{}, error) {
	w.mu.Lock()
	if len(w.records) != 1 {
		w.mu.Unlock()
		return nil, fmt.Errorf("select a crawl through /crawls")
	}
	var r *CrawlRecord
	for _, v := range w.records {
		r = v
	}
	slot := w.slots[r.ID]
	w.mu.Unlock()
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.supervisor == nil {
		if action == "status" {
			return map[string]interface{}{"state": "stopped"}, nil
		}
		return nil, fmt.Errorf("start this crawl first")
	}
	return slot.supervisor.Handle(ctx, action, raw)
}
func ServeWorkspace(ctx context.Context, options Options) error {
	w, err := NewWorkspace(ctx, options)
	if err != nil {
		return err
	}
	defer w.Close()
	a, err := DefaultApplication(options)
	if err != nil {
		return err
	}
	a.Config.API.StaticFiles = ui.Assets()
	config.Apply(a.Config)
	server, cleanup, err := api.NewServerWithOperations(a.Config.API, w)
	if err != nil {
		return err
	}
	defer cleanup()
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	failures := make(chan error, 2)
	go func() { failures <- server.Serve(listener) }()
	streamer := gateway.NewClientStreamer()
	if a.Config.Streamer.Enabled {
		go func() { failures <- streamer.Start() }()
		defer func() {
			if c, ok := streamer.(interface{ Close() error }); ok {
				c.Close()
			}
		}()
	}
	fmt.Printf("Delver workspace: http://%s\n", listener.Addr())
	select {
	case <-ctx.Done():
	case err = <-failures:
		if err == http.ErrServerClosed {
			err = nil
		}
	}
	stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e := server.Shutdown(stop); err == nil {
		err = e
	}
	return err
}
