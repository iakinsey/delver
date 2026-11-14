package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/api"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/gateway"
	"github.com/iakinsey/delver/instrument"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/worker"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
	"sync"
	"time"
)

type ResourceFactory func(json.RawMessage) (interface{}, error)
type WorkerFactory func(json.RawMessage, map[string]interface{}) (worker.Worker, error)

var runToken = make(chan struct{}, 1)
var registryMutex sync.RWMutex
var resourceFactories = map[string]ResourceFactory{}
var workerFactories = map[string]WorkerFactory{}

func RegisterResource(name string, factory ResourceFactory) {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	resourceFactories[name] = factory
}
func RegisterWorker(name string, factory WorkerFactory) {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	workerFactories[name] = factory
}
func Load(path string) (config.Application, error) {
	f, err := os.Open(path)
	if err != nil {
		return config.Application{}, err
	}
	defer f.Close()
	return Decode(f)
}
func Decode(reader io.Reader) (config.Application, error) {
	data, err := io.ReadAll(io.LimitReader(reader, 4<<20))
	if err != nil {
		return config.Application{}, err
	}
	var raw interface{}
	if err = json.Unmarshal(data, &raw); err != nil {
		return config.Application{}, err
	}
	if err = normalizeDurations(raw); err != nil {
		return config.Application{}, err
	}
	data, err = json.Marshal(raw)
	if err != nil {
		return config.Application{}, err
	}
	app := config.Application{Config: config.LoadConfig()}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&app); err != nil {
		return app, err
	}
	return app, Validate(app)
}
func normalizeDurations(value interface{}) error {
	switch v := value.(type) {
	case map[string]interface{}:
		for key, child := range v {
			switch key {
			case "timeout", "interval", "delay", "request_delay", "expiration", "clear_expired_delay", "gc_interval", "save_interval", "default_save_interval", "rotate_after", "recrawl_after", "shutdown_timeout":
				if text, ok := child.(string); ok {
					duration, err := time.ParseDuration(text)
					if err != nil {
						return fmt.Errorf("%s: %w", key, err)
					}
					v[key] = int64(duration)
				}
			}
			if err := normalizeDurations(child); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, child := range v {
			if err := normalizeDurations(child); err != nil {
				return err
			}
		}
	}
	return nil
}
func Validate(app config.Application) error {
	resources := map[string]config.Resource{}
	workers := map[string]bool{}
	knownResources := map[string]bool{"file_queue": true, "channel_queue": true, "timer": true, "bloom_filter": true, "rolling_bloom_filter": true, "persistent_map": true, "multi_host_map": true, "filesystem_object_store": true, "crawl_queue": true}
	knownWorkers := map[string]bool{"http_fetcher": true, "composite_extractor": true, "dfs_basic_accumulator": true, "news_accumulator": true, "dfs_basic_publisher": true, "fixed_seed_publisher": true, "rss_feed_publisher": true, "transformer": true, "jsonl_sink": true}
	registryMutex.RLock()
	defer registryMutex.RUnlock()
	if app.Config.Workers.WorkerCounts < 1 || app.Config.ShutdownTimeout <= 0 {
		return fmt.Errorf("worker_counts and shutdown_timeout must be positive")
	}
	for _, r := range app.Resources {
		if r.Name == "" {
			return fmt.Errorf("resource name is required")
		}
		if _, ok := resources[r.Name]; ok {
			return fmt.Errorf("duplicate resource %s", r.Name)
		}
		if !knownResources[r.Type] && resourceFactories[r.Type] == nil {
			return fmt.Errorf("unknown resource type %s", r.Type)
		}
		resources[r.Name] = r
	}
	for _, w := range app.Workers {
		if w.Name == "" || workers[w.Name] {
			return fmt.Errorf("missing or duplicate worker name %q", w.Name)
		}
		workers[w.Name] = true
		if !knownWorkers[w.Type] && workerFactories[w.Type] == nil {
			return fmt.Errorf("unknown worker type %s", w.Type)
		}
		if w.Count < 0 {
			return fmt.Errorf("worker %s has negative count", w.Name)
		}
		if w.Manager != "" && w.Manager != "worker" && w.Manager != "job" {
			return fmt.Errorf("worker %s has unknown manager", w.Name)
		}
		if w.Manager == "job" && w.Interval <= 0 {
			return fmt.Errorf("worker %s requires positive interval", w.Name)
		}
		if w.Manager != "job" {
			r, ok := resources[w.Inbox]
			if !ok || (!isQueue(r.Type) && resourceFactories[r.Type] == nil) {
				return fmt.Errorf("worker %s requires a queue inbox", w.Name)
			}
		}
		if w.Outbox != "" {
			r, ok := resources[w.Outbox]
			if !ok || (!isQueue(r.Type) && resourceFactories[r.Type] == nil) {
				return fmt.Errorf("worker %s has invalid outbox", w.Name)
			}
		}
	}
	if err := validateMessageFlow(app); err != nil {
		return err
	}
	return validateParameters(app, resources)
}
func isQueue(kind string) bool {
	return kind == "file_queue" || kind == "channel_queue" || kind == "timer" || kind == "crawl_queue"
}
func Run(ctx context.Context, app config.Application) (err error) {
	if err = Validate(app); err != nil {
		return err
	}
	select {
	case runToken <- struct{}{}:
	default:
		return fmt.Errorf("an application is already running")
	}
	releaseRun := true
	var shutdownDone chan struct{}
	defer func() {
		if releaseRun {
			<-runToken
		} else {
			go func() { <-shutdownDone; <-runToken }()
		}
	}()
	instrument.Stop()
	defer instrument.Stop()
	config.Apply(app.Config)
	gateway.ResetClientStreamer()
	if err := ctx.Err(); err != nil {
		return err
	}
	shutdownStarted := false
	prepared := &preparedApplication{app: app, resources: map[string]interface{}{}, workers: map[string]worker.WorkerManager{}}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("application construction: %v", recovered)
		}
		if err != nil && !shutdownStarted {
			for _, m := range prepared.workers {
				m.Stop()
			}
			for _, r := range prepared.resources {
				switch c := r.(type) {
				case interface{ Close() }:
					c.Close()
				case io.Closer:
					c.Close()
				case queue.Queue:
					c.Stop()
				}
			}
		}
	}()
	for _, r := range app.Resources {
		CreateResource(r, prepared)
	}
	for _, w := range app.Workers {
		CreateWorker(w, prepared)
	}
	retained := map[types.UUID]bool{}
	canReconcile := true
	for _, r := range prepared.resources {
		if source, ok := r.(interface{ ReferencedObjects() ([]types.UUID, error) }); ok {
			refs, err := source.ReferencedObjects()
			if err != nil {
				canReconcile = false
				break
			}
			for _, key := range refs {
				retained[key] = true
			}
		}
	}
	if canReconcile {
		for _, r := range prepared.resources {
			if store, ok := r.(interface {
				Reconcile(map[types.UUID]bool) error
			}); ok {
				if err := store.Reconcile(retained); err != nil {
					return err
				}
			}
		}
	}

	serviceErrors := make(chan error, 2)
	server, cleanup, err := api.NewServer(app.Config.API)
	if err != nil {
		return err
	}
	defer cleanup()
	if server != nil {
		listener, listenErr := net.Listen("tcp", server.Addr)
		if listenErr != nil {
			return listenErr
		}
		go func() { serviceErrors <- server.Serve(listener) }()
		defer server.Close()
	}
	streamer := gateway.NewClientStreamer()
	if app.Config.Streamer.Enabled {
		go func() { serviceErrors <- streamer.Start() }()
		defer func() {
			if c, ok := streamer.(io.Closer); ok {
				c.Close()
			}
		}()
	}
	used := map[string]bool{}
	for _, w := range app.Workers {
		used[w.Inbox] = true
	}
	started := []queue.Queue{}
	for name, r := range prepared.resources {
		if q, ok := r.(queue.Queue); ok && used[name] {
			if err = q.Start(); err != nil {
				for _, q := range started {
					q.Stop()
				}
				return err
			}
			started = append(started, q)
		}
	}
	if app.Config.Workers.Enabled {
		for _, w := range app.Workers {
			count := w.Count
			if count == 0 {
				count = app.Config.Workers.WorkerCounts
				if w.Manager == "job" {
					count = 1
				}
			}
			for i := 0; i < count; i++ {
				go prepared.workers[w.Name].Start()
			}
		}
	}
	var serviceErr error
	select {
	case <-ctx.Done():
	case serviceErr = <-serviceErrors:
		if serviceErr == http.ErrServerClosed {
			serviceErr = nil
		}
	}
	shutdownStarted = true
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), app.Config.ShutdownTimeout)
	defer cancelShutdown()
	done := make(chan struct{})
	shutdownDone = done
	go func() {
		var wg sync.WaitGroup
		for _, m := range prepared.workers {
			wg.Add(1)
			go func(m worker.WorkerManager) { defer wg.Done(); m.Stop() }(m)
		}
		wg.Wait()
		for _, q := range started {
			q.Stop()
		}
		for _, r := range prepared.resources {
			if _, ok := r.(queue.Queue); ok {
				if c, ok := r.(io.Closer); ok {
					c.Close()
				}
				continue
			}
			switch c := r.(type) {
			case interface{ Close() }:
				c.Close()
			case io.Closer:
				c.Close()
			}
		}
		if server != nil {
			server.Shutdown(shutdownCtx)
		}
		if c, ok := streamer.(io.Closer); ok {
			c.Close()
		}
		close(done)
	}()
	select {
	case <-done:
		return serviceErr
	case <-shutdownCtx.Done():
		releaseRun = false
		return fmt.Errorf("shutdown exceeded %s", app.Config.ShutdownTimeout)
	}
}
func assignResource(target reflect.Value, value interface{}) error {
	if value == nil {
		return nil
	}
	v := reflect.ValueOf(value)
	if !v.Type().AssignableTo(target.Type()) {
		return fmt.Errorf("resource %s cannot be assigned to %s", v.Type(), target.Type())
	}
	target.Set(v)
	return nil
}
