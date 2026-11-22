package application

import (
	"context"
	"encoding/json"
	"github.com/armon/go-metrics"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/report"
	"github.com/iakinsey/delver/worker"
)

// RuntimeDependencies are scoped to a single crawl. Factories should capture
// these settings rather than consulting process-wide config during execution.
type RuntimeDependencies struct {
	Reporter      report.Writer
	Configuration config.Config
	Metrics       metrics.MetricSink
}
type RuntimeResourceFactory func(context.Context, json.RawMessage, RuntimeDependencies) (interface{}, error)
type RuntimeWorkerFactory func(context.Context, json.RawMessage, map[string]interface{}, RuntimeDependencies) (worker.Worker, error)

func RegisterResourceWithRuntime(name string, factory RuntimeResourceFactory, capabilities ...ProtectionCapability) {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	if factory == nil {
		delete(resourceFactories, name)
		delete(resourceProtection, name)
		delete(resourceProtectionCleaners, name)
	} else {
		resourceFactories[name] = factory
		resourceProtection[name], resourceProtectionCleaners[name] = declaredProtection(capabilities)
	}
}
func RegisterWorkerWithRuntime(name string, factory RuntimeWorkerFactory, capabilities ...ProtectionCapability) {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	if factory == nil {
		delete(workerFactories, name)
		delete(workerProtection, name)
		delete(workerProtectionCleaners, name)
	} else {
		workerFactories[name] = factory
		workerProtection[name], workerProtectionCleaners[name] = declaredProtection(capabilities)
	}
}
func (p *preparedApplication) dependencies() RuntimeDependencies {
	sink := p.metrics
	if sink == nil {
		sink = &metrics.BlackholeSink{}
	}
	var reporter report.Writer
	if p.reporting != nil {
		reporter = p.reporting
	}
	return RuntimeDependencies{Reporter: reporter, Configuration: p.app.Config, Metrics: sink}
}
