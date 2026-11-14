package instrument

import (
	"context"
	"encoding/json"
	log "github.com/sirupsen/logrus"
	"time"

	"github.com/armon/go-metrics"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/pkg/errors"
)

var stopMetrics context.CancelFunc
var metric metrics.MetricSink = &metrics.BlackholeSink{}

type encoder struct {
	ctx              context.Context
	transformerQueue queue.Queue
}

func NewMetricsEncoder(transformerQueue queue.Queue) metrics.Encoder {
	return &encoder{
		ctx:              context.Background(),
		transformerQueue: transformerQueue,
	}
}

func (s *encoder) Encode(args interface{}) error {
	m := args.(metrics.MetricsSummary)
	out := make([]types.Metric, 0)
	t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", m.Timestamp)

	if err != nil {
		return err
	}

	for _, g := range m.Gauges {
		out = append(out, types.Metric{
			Key:   g.Name,
			When:  t.Unix(),
			Value: float64(g.Value),
		})
	}

	for _, c := range m.Counters {
		out = append(out, types.Metric{
			Key:   c.Name,
			When:  t.Unix(),
			Value: c.Sum,
		})
	}

	for _, s := range m.Samples {
		out = append(out, types.Metric{
			Key:   s.Name,
			When:  t.Unix(),
			Value: s.Mean,
		})
	}

	if len(out) == 0 {
		return nil
	}
	if b, err := json.Marshal(out); err != nil {
		return errors.Wrap(err, "failed to serialize metrics")
	} else {
		message := types.Message{
			ID:          string(types.NewV4()),
			MessageType: types.MetricType,
			Message:     json.RawMessage(b),
		}
		if q, ok := s.transformerQueue.(interface {
			PutContext(context.Context, types.Message, int) error
		}); ok {
			return q.PutContext(s.ctx, message, 0)
		}
		return s.transformerQueue.Put(message, 0)
	}

}

func LoadMetrics(transformerQueue queue.Queue) metrics.MetricSink {
	conf := config.Get().Metrics

	if !conf.Enabled {
		return &metrics.BlackholeSink{}
	}

	m := metrics.NewInmemSink(time.Second, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	stopMetrics = cancel

	encoder := NewMetricsEncoder(transformerQueue).(*encoder)
	encoder.ctx = ctx
	go exportIntervals(ctx, m, encoder)

	return m
}

type Encoder interface {
	Encode(interface{}) error
}

func SetMetrics(transformerQueue queue.Queue) {
	Stop()
	metric = LoadMetrics(transformerQueue)
}

func GetMetrics() metrics.MetricSink {
	return metric
}

func Stop() {
	if stopMetrics != nil {
		stopMetrics()
		stopMetrics = nil
	}
	metric = &metrics.BlackholeSink{}
}

func exportIntervals(ctx context.Context, sink *metrics.InmemSink, encoder *encoder) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var exported time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			intervals := sink.Data()
			for _, interval := range intervals[:len(intervals)-1] {
				if !interval.Interval.After(exported) {
					continue
				}
				interval.RLock()
				summary := metrics.MetricsSummary{Timestamp: interval.Interval.UTC().String()}
				for _, gauge := range interval.Gauges {
					summary.Gauges = append(summary.Gauges, gauge)
				}
				for _, counter := range interval.Counters {
					counter.Mean = counter.AggregateSample.Mean()
					summary.Counters = append(summary.Counters, counter)
				}
				for _, sample := range interval.Samples {
					sample.Mean = sample.AggregateSample.Mean()
					summary.Samples = append(summary.Samples, sample)
				}
				interval.RUnlock()
				if err := encoder.Encode(summary); err != nil {
					if ctx.Err() == nil {
						log.Error(err)
					}
					break
				}
				exported = interval.Interval
			}
		}
	}
}
