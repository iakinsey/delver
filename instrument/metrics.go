package instrument

import (
	"context"
	"encoding/json"
	"time"

	"github.com/armon/go-metrics"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
)

var metric metrics.MetricSink = &metrics.BlackholeSink{}

type encoder struct {
	transformerQueue queue.Queue
}

func NewMetricsEncoder(transformerQueue queue.Queue) metrics.Encoder {
	return &encoder{
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
			Value: int64(g.Value),
		})
	}

	for _, c := range m.Counters {
		out = append(out, types.Metric{
			Key:   c.Name,
			When:  t.Unix(),
			Value: int64(c.Mean),
		})
	}

	for _, s := range m.Samples {
		out = append(out, types.Metric{
			Key:   s.Name,
			When:  t.Unix(),
			Value: int64(s.Mean),
		})
	}

	if b, err := json.Marshal(out); err != nil {
		return errors.Wrap(err, "failed to serialize metrics")
	} else {
		s.transformerQueue.Put(types.Message{
			ID:          string(types.NewV4()),
			MessageType: types.MetricType,
			Message:     json.RawMessage(b),
		}, 0)
	}

	return nil
}

func LoadMetrics(transformerQueue queue.Queue) metrics.MetricSink {
	conf := config.Get().Metrics

	if !conf.Enabled {
		return &metrics.BlackholeSink{}
	}

	m := metrics.NewInmemSink(time.Second, time.Minute)
	ctx := util.ContextWithSigterm(context.Background())

	go m.Stream(ctx, NewMetricsEncoder(transformerQueue))

	return m
}

type Encoder interface {
	Encode(interface{}) error
}

func SetMetrics(transformerQueue queue.Queue) {
	metric = LoadMetrics(transformerQueue)
}

func GetMetrics() metrics.MetricSink {
	return metric
}
