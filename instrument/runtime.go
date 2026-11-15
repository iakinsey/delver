package instrument

import (
	"context"
	"github.com/armon/go-metrics"
	"github.com/iakinsey/delver/queue"
	"time"
)

// NewRuntimeMetrics owns its exporter independently of other crawler runtimes.
func NewRuntimeMetrics(parent context.Context, enabled bool, q queue.Queue) (metrics.MetricSink, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	if !enabled || q == nil {
		return &metrics.BlackholeSink{}, cancel
	}
	sink := metrics.NewInmemSink(time.Second, time.Minute)
	go exportIntervals(ctx, sink, &encoder{ctx: ctx, transformerQueue: q})
	return sink, cancel
}
