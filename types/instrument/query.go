package instrument

import "time"

type MetricsQuery struct {
	Key    string `json:"key"`
	Start  int64  `json:"start"`
	End    int64  `json:"end"`
	Agg    string `json:"agg"`
	Window int64  `json:"window"`
}

type Metric struct {
	When  time.Time `json:"when,omitempty"`
	Value int64     `json:"value"`
}
