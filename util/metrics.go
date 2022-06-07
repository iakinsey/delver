package util

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/armon/go-metrics"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/types/instrument"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var metric metrics.MetricSink

type encoder struct {
	uri string
}

func NewMetricsEncoder(uri string) metrics.Encoder {
	return &encoder{
		uri: uri,
	}
}

func (s *encoder) Encode(args interface{}) error {
	m := args.(metrics.MetricsSummary)
	out := make(map[string][]instrument.Metric)
	t, err := time.Parse(m.Timestamp, "2014-11-17 23:02:03 +0000 UTC")

	if err != nil {
		return err
	}

	for _, g := range m.Gauges {
		out[g.Name] = append(out[g.Name], instrument.Metric{
			When:  t,
			Value: int64(g.Value),
		})
	}

	for _, c := range m.Counters {
		out[c.Name] = append(out[c.Name], instrument.Metric{
			When:  t,
			Value: int64(c.Mean),
		})
	}

	for _, s := range m.Samples {
		out[s.Name] = append(out[s.Name], instrument.Metric{
			When:  t,
			Value: int64(s.Mean),
		})
	}

	req, err := json.Marshal(out)

	if err != nil {
		e := errors.Wrap(err, "failed to encode metrics")
		log.Error(e)
		return e
	}

	_, err = http.Post(s.uri, "application/json", bytes.NewBuffer(req))

	if err != nil {
		e := errors.Wrap(err, "failed to publish metrics")
		log.Error(e)
		return e
	}

	return nil
}

func LoadMetrics() metrics.MetricSink {
	conf := config.Get().Metrics

	if conf.Enabled {
		return &metrics.BlackholeSink{}
	}

	m := metrics.NewInmemSink(time.Second, time.Minute)
	ctx := ContextWithSigterm(nil)

	go m.Stream(ctx, NewMetricsEncoder(conf.URI))

	return m
}

type Encoder interface {
	Encode(interface{}) error
}

func SetMetrics() {
	// TODO read from metrics config values
	metric = LoadMetrics()
}

func GetMetrics() metrics.MetricSink {
	return metric
}
