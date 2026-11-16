// Package flow provides protocol-independent execution, pressure and retry policy.
package flow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"time"
)

type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {

	return json.Marshal(time.Duration(d).String())
}
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		if s == "inherit" {
			*d = 0
			return nil
		}
		v, e := time.ParseDuration(s)
		*d = Duration(v)
		return e
	}
	var n int64
	if e := json.Unmarshal(b, &n); e != nil {
		return e
	}
	*d = Duration(n)
	return nil
}
func (d Duration) Value() time.Duration { return time.Duration(d) }

type Policy struct {
	MetricsSaveInterval   Duration `json:"metrics_save_interval"`
	ProcessOriginLimit    int      `json:"process_origin_limit"`
	ProcessOriginDelay    Duration `json:"process_origin_delay"`
	Mode                  string   `json:"mode"`
	Concurrency           int      `json:"concurrency"`
	Minimum               int      `json:"minimum"`
	Maximum               int      `json:"maximum"`
	ProcessLimit          int      `json:"process_limit"`
	SampleInterval        Duration `json:"sample_interval"`
	EvaluationWindow      Duration `json:"evaluation_window"`
	MinSamples            int      `json:"min_samples"`
	AdjustmentCooldown    Duration `json:"adjustment_cooldown"`
	IncreaseStep          int      `json:"increase_step"`
	DecreaseFactor        float64  `json:"decrease_factor"`
	EWMAAlpha             float64  `json:"ewma_alpha"`
	LatencyInflation      float64  `json:"latency_inflation"`
	MinimumGain           float64  `json:"minimum_gain"`
	OverloadFailureRatio  float64  `json:"overload_failure_ratio"`
	MemoryHighMB          int      `json:"memory_high_mb"`
	MemoryLowMB           int      `json:"memory_low_mb"`
	CPUHighPercent        float64  `json:"cpu_high_percent"`
	CPULowPercent         float64  `json:"cpu_low_percent"`
	MinFreeDiskMB         int      `json:"min_free_disk_mb"`
	QueueLowMultiplier    float64  `json:"queue_low_multiplier"`
	QueueTargetMultiplier float64  `json:"queue_target_multiplier"`
	QueueHardLimit        int      `json:"queue_hard_limit"`
	RefillBatch           int      `json:"refill_batch"`
	CandidateWindow       int      `json:"candidate_window"`
	DownstreamHigh        int      `json:"downstream_high"`
	DownstreamLow         int      `json:"downstream_low"`
	FailureThreshold      int      `json:"failure_threshold"`
	HostCooldown          Duration `json:"host_cooldown"`
	HostCooldownMax       Duration `json:"host_cooldown_max"`
	RecoveryProbeInterval Duration `json:"recovery_probe_interval"`
	RetryShare            float64  `json:"retry_share"`
	ConnectTimeout        Duration `json:"connect_timeout"`
	TLSTimeout            Duration `json:"tls_timeout"`
	HeaderTimeout         Duration `json:"header_timeout"`
	BodyTimeout           Duration `json:"body_timeout"`
	MetricsWindow         Duration `json:"metrics_window"`
	MetricsSamples        int      `json:"metrics_samples"`
}
type Option struct {
	Key     string  `json:"key"`
	Label   string  `json:"label"`
	Group   string  `json:"group"`
	Kind    string  `json:"kind"`
	Minimum float64 `json:"min"`
	Maximum float64 `json:"max"`
}

func Options() []Option {
	return []Option{
		{"metrics_save_interval", "Metrics save interval", "Metrics", "duration", .01, 3600},
		{"process_origin_limit", "Shared requests per host", "Execution", "integer", 1, 8192},
		{"process_origin_delay", "Shared delay per host", "Execution", "duration", 0, 86400},
		{"concurrency", "Concurrent requests overall", "Execution", "integer", 1, 8192}, {"process_limit", "Process-wide request ceiling", "Execution", "integer", 1, 8192},
		{"minimum", "Minimum concurrency", "Adaptive limits", "integer", 1, 8192}, {"maximum", "Maximum concurrency", "Adaptive limits", "integer", 1, 8192},
		{"sample_interval", "Pressure sampling interval", "Adaptive control", "duration", .01, 3600}, {"evaluation_window", "Evaluation window", "Adaptive control", "duration", .01, 86400}, {"min_samples", "Minimum completed samples", "Adaptive control", "integer", 1, 1000000}, {"adjustment_cooldown", "Adjustment cooldown", "Adaptive control", "duration", .01, 86400},
		{"increase_step", "Concurrency increase step", "Adaptive control", "integer", 1, 8192}, {"decrease_factor", "Concurrency decrease multiplier", "Adaptive control", "number", .01, .99}, {"ewma_alpha", "Latency smoothing weight", "Adaptive control", "number", .01, 1}, {"latency_inflation", "Allowed latency inflation", "Adaptive control", "number", 1, 100}, {"minimum_gain", "Minimum throughput gain", "Adaptive control", "number", 0, 1}, {"overload_failure_ratio", "Proxy/local overload failure ratio", "Adaptive control", "number", .01, 1},
		{"memory_high_mb", "Memory high watermark (MiB)", "Resource limits", "integer", 1, 1048576}, {"memory_low_mb", "Memory resume watermark (MiB)", "Resource limits", "integer", 0, 1048576}, {"cpu_high_percent", "CPU high watermark (%)", "Resource limits", "number", 1, 100}, {"cpu_low_percent", "CPU resume watermark (%)", "Resource limits", "number", 0, 99}, {"min_free_disk_mb", "Minimum free disk space (MiB)", "Resource limits", "integer", 1, 1048576},
		{"queue_low_multiplier", "Fetch refill low watermark multiplier", "Dispatch and backpressure", "number", .1, 100}, {"queue_target_multiplier", "Fetch queue target multiplier", "Dispatch and backpressure", "number", .1, 100}, {"queue_hard_limit", "Fetch queue hard ceiling", "Dispatch and backpressure", "integer", 1, 1000000}, {"refill_batch", "Maximum refill batch", "Dispatch and backpressure", "integer", 1, 100000}, {"candidate_window", "Candidate selection window", "Dispatch and backpressure", "integer", 1, 1000000}, {"downstream_high", "Downstream queue high watermark", "Dispatch and backpressure", "integer", 1, 1000000}, {"downstream_low", "Downstream queue resume watermark", "Dispatch and backpressure", "integer", 0, 1000000},
		{"failure_threshold", "Failures before host cooldown", "Retries and recovery", "integer", 1, 1000}, {"host_cooldown", "Initial host cooldown", "Retries and recovery", "duration", .01, 604800}, {"host_cooldown_max", "Maximum host cooldown", "Retries and recovery", "duration", .01, 31536000}, {"recovery_probe_interval", "Recovery probe spacing", "Retries and recovery", "duration", .01, 604800}, {"retry_share", "Retry scheduling share", "Retries and recovery", "number", .01, 1},
		{"connect_timeout", "Connection timeout", "Network phases", "inherit_duration", 0, 86400}, {"tls_timeout", "TLS handshake timeout", "Network phases", "inherit_duration", 0, 86400}, {"header_timeout", "Response header timeout", "Network phases", "inherit_duration", 0, 86400}, {"body_timeout", "Response body timeout", "Network phases", "inherit_duration", 0, 86400},
		{"metrics_window", "Metric rate window", "Metrics", "duration", .01, 86400}, {"metrics_samples", "Maximum samples per metric", "Metrics", "integer", 16, 65536},
	}
}
func Default() Policy {
	n := runtime.NumCPU()
	memory := defaultMemoryMB()
	return Policy{MetricsSaveInterval: Duration(10 * time.Second), ProcessOriginLimit: 2, ProcessOriginDelay: Duration(100 * time.Millisecond), Mode: "fixed", Concurrency: n * 4, Minimum: 1, Maximum: n * 16, ProcessLimit: n * 16, SampleInterval: Duration(time.Second), EvaluationWindow: Duration(30 * time.Second), MinSamples: 20, AdjustmentCooldown: Duration(30 * time.Second), IncreaseStep: 1, DecreaseFactor: .8, EWMAAlpha: .2, LatencyInflation: 1.5, MinimumGain: .02, OverloadFailureRatio: .2, MemoryHighMB: memory, MemoryLowMB: memory * 3 / 4, CPUHighPercent: 90, CPULowPercent: 70, MinFreeDiskMB: 64, QueueLowMultiplier: 1, QueueTargetMultiplier: 2, QueueHardLimit: 4096, RefillBatch: 256, CandidateWindow: 4096, DownstreamHigh: 128, DownstreamLow: 64, FailureThreshold: 3, HostCooldown: Duration(30 * time.Second), HostCooldownMax: Duration(30 * time.Minute), RecoveryProbeInterval: Duration(30 * time.Second), RetryShare: .2, MetricsWindow: Duration(time.Minute), MetricsSamples: 1024}
}
func Legacy() Policy { p := Default(); p.Concurrency = 4; p.Maximum = 4; return p }
func (p Policy) Validate() error {
	if p.Mode != "fixed" && p.Mode != "adaptive" {
		return fmt.Errorf("fetch mode must be fixed or adaptive")
	}
	values := map[string]float64{}
	rv := reflect.ValueOf(p)
	rt := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		v := rv.Field(i)
		if v.Kind() == reflect.String {
			continue
		}
		f := float64(0)
		if v.Kind() == reflect.Float64 {
			f = v.Float()
		} else {
			f = float64(v.Int())
		}
		if v.Type() == reflect.TypeOf(Duration(0)) {
			f /= float64(time.Second)
		}
		values[rt.Field(i).Tag.Get("json")] = f
	}
	for _, o := range Options() {
		v := values[o.Key]
		if math.IsNaN(v) || math.IsInf(v, 0) || v < o.Minimum || v > o.Maximum {
			return fmt.Errorf("%s must be between %g and %g", o.Label, o.Minimum, o.Maximum)
		}
	}
	if p.Minimum > p.Concurrency || p.Concurrency > p.Maximum {
		return fmt.Errorf("fetch minimum must not exceed initial concurrency, which must not exceed maximum")
	}
	if p.QueueLowMultiplier >= p.QueueTargetMultiplier {
		return fmt.Errorf("fetch queue target must exceed its low watermark")
	}
	if p.QueueHardLimit < p.Concurrency || p.RefillBatch > p.QueueHardLimit || p.CandidateWindow < p.RefillBatch {
		return fmt.Errorf("fetch queue ceiling must cover initial concurrency and refill batch; candidate window must cover batch")
	}
	if p.Mode == "adaptive" && p.QueueHardLimit < p.Maximum {
		return fmt.Errorf("fetch queue ceiling must cover adaptive maximum")
	}
	if p.DownstreamLow >= p.DownstreamHigh || p.MemoryLowMB >= p.MemoryHighMB || p.CPULowPercent >= p.CPUHighPercent {
		return fmt.Errorf("resume watermarks must be below high watermarks")
	}
	if p.HostCooldownMax < p.HostCooldown || p.EvaluationWindow < p.SampleInterval {
		return fmt.Errorf("maximum cooldown and evaluation window must cover their initial intervals")
	}
	return nil
}
func (p *Policy) UnmarshalJSON(b []byte) error {
	type alias Policy
	var a alias
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&a); err != nil {
		return err
	}
	*p = Policy(a)
	return p.Validate()
}
func (p Policy) PoolSize() int {
	if p.Mode == "adaptive" {
		return p.Maximum
	}
	return p.Concurrency
}
