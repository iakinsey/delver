package application

import (
	"context"
	"encoding/json"
	"github.com/armon/go-metrics"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/worker"
)

// RuntimeDependencies are scoped to a single crawl. Factories should capture
// these settings rather than consulting process-wide config during execution.
type RuntimeDependencies struct {
	Configuration config.Config
	Metrics       metrics.MetricSink
}
type RuntimeResourceFactory func(context.Context, json.RawMessage, RuntimeDependencies) (interface{}, error)
type RuntimeWorkerFactory func(context.Context, json.RawMessage, map[string]interface{}, RuntimeDependencies) (worker.Worker, error)

var runtimeResourceFactories = map[string]RuntimeResourceFactory{}
var runtimeWorkerFactories = map[string]RuntimeWorkerFactory{}

func RegisterResourceWithRuntime(name string, factory RuntimeResourceFactory) {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	runtimeResourceFactories[name] = factory
	if factory == nil {
		resourceFactories[name] = nil
	} else {
		resourceFactories[name] = func(raw json.RawMessage) (interface{}, error) {
			return factory(context.Background(), raw, RuntimeDependencies{Configuration: config.Get(), Metrics: &metrics.BlackholeSink{}})
		}
	}
}
func RegisterWorkerWithRuntime(name string, factory RuntimeWorkerFactory) {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	runtimeWorkerFactories[name] = factory
	if factory == nil {
		workerFactories[name] = nil
	} else {
		workerFactories[name] = func(raw json.RawMessage, resources map[string]interface{}) (worker.Worker, error) {
			return factory(context.Background(), raw, resources, RuntimeDependencies{Configuration: config.Get(), Metrics: &metrics.BlackholeSink{}})
		}
	}
}
func (p *preparedApplication) dependencies() RuntimeDependencies {
	sink := p.metrics
	if sink == nil {
		sink = &metrics.BlackholeSink{}
	}
	return RuntimeDependencies{Configuration: p.app.Config, Metrics: sink}
}
