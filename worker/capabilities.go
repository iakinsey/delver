package worker

import (
	"github.com/armon/go-metrics"
	"github.com/iakinsey/delver/flow"
)

// Optional capabilities are resolved once during application preparation.
type MetricAware interface{ SetMetricSink(metrics.MetricSink) }
type FlowAware interface {
	SetFlow(*flow.Controller, flow.Policy)
}
type Pausable interface{ Pause(bool) }
type ActivitySource interface{ Activity() Activity }
type HealthSource interface{ Health() string }
