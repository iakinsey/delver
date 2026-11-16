package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/api"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/instrument"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/worker"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"
)

type ResourceFactory func(json.RawMessage) (interface{}, error)
type WorkerFactory func(json.RawMessage, map[string]interface{}) (worker.Worker, error)

var runToken = make(chan struct{}, 1)
var registryMutex sync.RWMutex
var resourceFactories = map[string]RuntimeResourceFactory{}
var workerFactories = map[string]RuntimeWorkerFactory{}

// Legacy factory signatures are adapted once, at registration.
func RegisterResource(name string, factory ResourceFactory) {
	if factory == nil {
		RegisterResourceWithRuntime(name, nil)
		return
	}
	RegisterResourceWithRuntime(name, func(_ context.Context, raw json.RawMessage, _ RuntimeDependencies) (interface{}, error) {
		return factory(raw)
	})
}
func RegisterWorker(name string, factory WorkerFactory) {
	if factory == nil {
		RegisterWorkerWithRuntime(name, nil)
		return
	}
	RegisterWorkerWithRuntime(name, func(_ context.Context, raw json.RawMessage, resources map[string]interface{}, _ RuntimeDependencies) (worker.Worker, error) {
		return factory(raw, resources)
	})
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
	data, err := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
	if err != nil {
		return config.Application{}, err
	}
	if len(data) > 4<<20 {
		return config.Application{}, fmt.Errorf("configuration exceeds 4 MiB limit")
	}
	var raw interface{}
	rawDecoder := json.NewDecoder(bytes.NewReader(data))
	rawDecoder.UseNumber()
	if err = rawDecoder.Decode(&raw); err != nil {
		return config.Application{}, err
	}
	stripObsoleteAccountConfig(raw)
	var trailing interface{}
	if err := rawDecoder.Decode(&trailing); err != io.EOF {
		return config.Application{}, fmt.Errorf("configuration must contain one JSON object")
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
			if key == "parameters" {
				kind, _ := v["type"].(string)
				switch kind {
				case "http_fetcher", "composite_extractor", "dfs_basic_accumulator", "news_accumulator", "dfs_basic_publisher", "fixed_seed_publisher", "rss_feed_publisher", "transformer", "jsonl_sink", "file_queue", "channel_queue", "timer", "bloom_filter", "rolling_bloom_filter", "persistent_map", "multi_host_map", "filesystem_object_store", "crawl_queue":
				default:
					continue
				}
			}
			switch key {
			case "timeout", "connect_timeout", "tls_timeout", "header_timeout", "body_timeout", "interval", "delay", "request_delay", "retry_delay", "retry_max_delay", "expiration", "clear_expired_delay", "gc_interval", "save_interval", "default_save_interval", "rotate_after", "recrawl_after", "shutdown_timeout":
				if text, ok := child.(string); ok {
					if text == "inherit" && (key == "connect_timeout" || key == "tls_timeout" || key == "header_timeout" || key == "body_timeout") {
						v[key] = int64(0)
						continue
					}
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
	if app.Config.Streamer.Enabled {
		return fmt.Errorf("legacy streaming output retired")
	}
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
		if w.Manager != "job" && resources[w.Inbox].Type == "timer" && knownWorkers[w.Type] && w.Type != "fixed_seed_publisher" && w.Type != "rss_feed_publisher" && w.Type != "dfs_basic_publisher" {
			return fmt.Errorf("worker %s cannot consume timer messages", w.Name)
		}
		if w.Manager != "job" {
			r, ok := resources[w.Inbox]
			if !ok || (!isQueue(r.Type) && resourceFactories[r.Type] == nil) {
				return fmt.Errorf("worker %s requires a queue inbox", w.Name)
			}
		}
		if w.Outbox != "" {
			r, ok := resources[w.Outbox]
			if ok && r.Type == "timer" {
				return fmt.Errorf("worker %s cannot publish to a timer", w.Name)
			}
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
	var resourcesDone <-chan struct{}
	if control, ok := ctx.Value(runtimeKey{}).(*runtimeControl); ok && control.resourcesDone != nil {
		defer func() {
			if resourcesDone == nil {
				close(control.resourcesDone)
			} else {
				go func() { <-resourcesDone; close(control.resourcesDone) }()
			}
		}()
	}
	if err = Validate(app); err != nil {
		return err
	}
	if app.Config.HTTPClient.IsolateProxySessions {
		key := app.Config.API.OperationsPath
		if key == "" {
			key = string(types.NewV4())
		}
		app.Config.HTTPClient.ProxySessionKey = key
	}
	releaseRun := true
	var shutdownDone chan struct{}
	if ctx.Value(runtimeKey{}) == nil {
		select {
		case runToken <- struct{}{}:
		default:
			return fmt.Errorf("an application is already running")
		}
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
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	shutdownStarted := false
	prepared := &preparedApplication{pressurePaths: append(pipelinePaths(app), filepath.Dir(app.Config.API.OperationsPath)), ctx: ctx, app: app, resources: map[string]interface{}{}, workers: map[string]worker.WorkerManager{}}
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
	if ctx.Value(runtimeKey{}) != nil {
		q, _ := prepared.resources[config.TransformerQueueName].(queue.Queue)
		sink, cancel := instrument.NewRuntimeMetrics(ctx, app.Config.Metrics.Enabled, q)
		defer cancel()
		prepared.metrics = sink
	}
	if app.Config.Fetch == nil {
		count := 0
		for _, definition := range app.Workers {
			if definition.Type == "http_fetcher" {
				n := definition.Count
				if n == 0 {
					n = app.Config.Workers.WorkerCounts
				}
				count += n
			}
		}
		if count > 0 {
			p := flow.Legacy()
			p.Concurrency = count
			p.Maximum = count
			if p.QueueHardLimit < count {
				p.QueueHardLimit = count
			}
			app.Config.Fetch = &p
			prepared.app.Config.Fetch = &p
		}
	}
	if app.Config.Fetch != nil {
		statePath := ""
		if app.Config.API.OperationsPath != "" {
			statePath = app.Config.API.OperationsPath + ".fetch.json"
		}
		controller, e := flow.New(ctx, string(types.NewV4()), *app.Config.Fetch, prepared.pressure, statePath)
		if e != nil {
			return e
		}
		defer controller.Close()
		prepared.flow = controller
		for _, resource := range prepared.resources {
			if target, ok := resource.(interface{ SetFlow(*flow.Controller) }); ok {
				target.SetFlow(controller)
			}
		}
		prepared.app.Config.HTTPClient.Flow = controller
		prepared.app.Config.HTTPClient.ConnectTimeout = app.Config.Fetch.ConnectTimeout.Value()
		prepared.app.Config.HTTPClient.TLSTimeout = app.Config.Fetch.TLSTimeout.Value()
		prepared.app.Config.HTTPClient.HeaderTimeout = app.Config.Fetch.HeaderTimeout.Value()
		prepared.app.Config.HTTPClient.BodyTimeout = app.Config.Fetch.BodyTimeout.Value()
	}
	for _, w := range app.Workers {
		CreateWorker(w, prepared)
		if control, ok := ctx.Value(runtimeKey{}).(*runtimeControl); ok && control.log != nil && w.Type == "http_fetcher" {
			if manager, ok := prepared.workers[w.Name].(interface {
				SetObserver(func(types.Message, []types.Message, error))
			}); ok {
				manager.SetObserver(control.log.record)
			}
		}
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

	serviceErrors := make(chan error, len(app.Resources)+2)
	apiConfig := app.Config.API
	supervised := ctx.Value(runtimeKey{}) != nil
	if supervised {
		apiConfig.Enabled = false
	}
	server, cleanup, err := api.NewServer(apiConfig)
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
	used := map[string]bool{}
	for _, w := range app.Workers {
		used[w.Inbox] = true
	}
	if control, ok := ctx.Value(runtimeKey{}).(*runtimeControl); ok && control.initialPaused {
		for _, q := range durableDispatch(prepared) {
			q.PauseDispatch(true)
		}
		for _, manager := range prepared.workers {
			manager.(interface{ Pause(bool) }).Pause(true)
		}
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
			if source, ok := q.(interface{ Failures() <-chan error }); ok {
				go func() {
					select {
					case err, open := <-source.Failures():
						if open && err != nil {
							serviceErrors <- err
						}
					case <-ctx.Done():
					}
				}()
			}
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
	if control, ok := ctx.Value(runtimeKey{}).(*runtimeControl); ok {
		control.mu.Lock()
		control.prepared = prepared
		control.mu.Unlock()
		defer func() { control.mu.Lock(); control.prepared = nil; control.mu.Unlock() }()
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
	resourcesDone = done
	shutdownErrors := &cleanupErrors{}
	go func() {
		var wg sync.WaitGroup
		for _, m := range prepared.workers {
			wg.Add(1)
			go func(m worker.WorkerManager) {
				defer wg.Done()
				shutdownErrors.run("worker", func() error { m.Stop(); return nil })
			}(m)
		}
		wg.Wait()
		for _, q := range started {
			shutdownErrors.run("queue", q.Stop)
		}
		if control, ok := ctx.Value(runtimeKey{}).(*runtimeControl); ok {
			// Workers and admission have settled; storage is still open for counts.
			if control.onFinalSnapshot != nil {
				control.onFinalSnapshot(control.snapshot())
			}
			control.mu.Lock()
			control.prepared = nil
			control.mu.Unlock()
		}
		for name, resource := range prepared.resources {
			if _, ok := resource.(queue.Queue); ok {
				if closer, ok := resource.(io.Closer); ok {
					shutdownErrors.run(name, closer.Close)
				}
				continue
			}
			switch closer := resource.(type) {
			case interface{ Close() }:
				shutdownErrors.run(name, func() error { closer.Close(); return nil })
			case io.Closer:
				shutdownErrors.run(name, closer.Close)
			}
		}
		if server != nil {
			shutdownErrors.run("api", func() error { return server.Shutdown(shutdownCtx) })
		}
		close(done)
	}()
	select {
	case <-done:
		if cleanupErr := shutdownErrors.err(); cleanupErr != nil {
			if serviceErr != nil {
				return fmt.Errorf("%v; %w", serviceErr, cleanupErr)
			}
			return cleanupErr
		}
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
