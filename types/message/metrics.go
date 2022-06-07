package message

import "github.com/armon/go-metrics"

type Metrics struct {
	Gauges   map[string]metrics.GaugeValue   `json:"gauges"`
	Points   map[string][]float32            `json:"points"`
	Counters map[string]metrics.SampledValue `json:"counters"`
	Samples  map[string]metrics.SampledValue `json:"samples"`
}
