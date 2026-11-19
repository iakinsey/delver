package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/require"
	"math"
	"runtime"
	"testing"
	"time"
)

func testPolicy() Policy {
	p := Default()
	p.Concurrency = 2
	p.Minimum = 1
	p.Maximum = 4
	p.IncreaseStep = 1
	p.SampleInterval = Duration(time.Hour)
	p.AdjustmentInterval = Duration(time.Hour)
	p.MinSamples = 1
	return p
}
func controller(t *testing.T, name string, p Policy, brokers ...*Broker) *Controller {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	broker := NewBroker(ExecutionLimits{Concurrency: 4, HostConcurrency: 2})
	if len(brokers) > 0 {
		broker = brokers[0]
	}
	c, err := New(WithBroker(ctx, broker), t.Name()+name, p, nil)
	require.NoError(t, err)
	t.Cleanup(func() { cancel(); c.Close() })
	return c
}
func TestExecutionCapsFairnessAndCancellation(t *testing.T) {
	p := testPolicy()
	shared := NewBroker(ExecutionLimits{Concurrency: 1, HostConcurrency: 2})
	a := controller(t, "a", p, shared)
	b := controller(t, "b", p, shared)
	release, err := a.Acquire(context.Background())
	require.NoError(t, err)
	got := make(chan string, 2)
	hold := make(chan struct{})
	go func() {
		r, e := b.Acquire(context.Background())
		if e == nil {
			got <- "b"
			<-hold
			r()
		}
	}()
	time.Sleep(10 * time.Millisecond)
	go func() {
		r, e := a.Acquire(context.Background())
		if e == nil {
			got <- "a"
			r()
		}
	}()
	release()
	require.Equal(t, "b", <-got)
	require.Equal(t, 1, b.Snapshot().ProcessActive)
	close(hold)
	require.Equal(t, "a", <-got)
	r, err := a.Acquire(context.Background())
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = b.Acquire(ctx)
	require.Error(t, err)
	r()
	require.Eventually(t, func() bool { return a.Snapshot().ProcessActive == 0 }, time.Second, time.Millisecond)
}
func TestPressureHysteresisAndRecovery(t *testing.T) {
	p := testPolicy()
	p.DownstreamHigh = 10
	p.DownstreamLow = 3
	c := controller(t, "", p)
	now := time.Now()
	pressure := Pressure{Queues: map[string]int64{"extract": 10}, DiskFreeMB: 1024}
	c.Evaluate(now, pressure)
	require.Contains(t, c.Snapshot().Reason, "downstream")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := c.Acquire(ctx)
	require.Error(t, err)
	pressure.Queues["extract"] = 6
	c.Evaluate(now.Add(time.Second), pressure)
	require.Contains(t, c.Snapshot().Reason, "downstream")
	pressure.Queues["extract"] = 2
	c.Evaluate(now.Add(2*time.Second), pressure)
	r, err := c.Acquire(context.Background())
	require.NoError(t, err)
	r()
	n, target, _, _ := c.DispatchBounds(0)
	require.Equal(t, 4, target)
	require.Equal(t, 4, n)
	pressure.DiskFreeMB = 1
	c.Evaluate(now.Add(3*time.Second), pressure)
	n, _, _, _ = c.DispatchBounds(0)
	require.Zero(t, n)
}
func TestAdaptiveLimitsAndDestinationFailures(t *testing.T) {
	p := testPolicy()
	p.Mode = "adaptive"
	p.DecreaseFactor = .5
	c := controller(t, "", p)
	start := time.Now()
	pressure := Pressure{FetchPending: 100, DiskFreeMB: 1024}
	c.saturated = true
	c.Record(200, true, true, "", 10*time.Millisecond, 10, 10)
	c.Evaluate(start.Add(2*time.Hour), pressure)
	require.Equal(t, 3, c.Effective())
	c.Record(0, false, false, "timeout", 10*time.Millisecond, 0, 0)
	c.Evaluate(start.Add(4*time.Hour), pressure)
	require.Equal(t, 3, c.Effective(), "unreachable destinations must not cause global overload reductions")
	c.Record(0, false, false, "proxy", 10*time.Millisecond, 0, 0)
	c.Evaluate(start.Add(6*time.Hour), pressure)
	require.Equal(t, 1, c.Effective())
	require.NotEmpty(t, c.Snapshot().LastAdjustment)
	for i := 0; i < 20; i++ {
		c.saturated = true
		c.Record(200, true, true, "", 10*time.Millisecond, 10, 10)
		c.Evaluate(start.Add(time.Duration(8+i*2)*time.Hour), pressure)
	}
	require.LessOrEqual(t, c.Effective(), p.Maximum)
	require.GreaterOrEqual(t, c.Effective(), p.Minimum)
}
func TestAllOptionBoundsAndStrictSchema(t *testing.T) {
	base := Default()
	base.Mode = "adaptive"
	for _, o := range Options() {
		o := o
		for _, upper := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-upper-%v", o.Key, upper), func(t *testing.T) {
				raw, _ := json.Marshal(base)
				var value map[string]interface{}
				require.NoError(t, json.Unmarshal(raw, &value))
				n := o.Minimum - 1
				if upper {
					n = o.Maximum + 1
				}
				if o.Kind == "duration" || o.Kind == "inherit_duration" {
					value[o.Key] = time.Duration(n * float64(time.Second)).String()
				} else {
					value[o.Key] = n
				}
				raw, _ = json.Marshal(value)
				var p Policy
				require.Error(t, json.Unmarshal(raw, &p))
			})
		}
	}
	raw, _ := json.Marshal(base)
	var p Policy
	require.NoError(t, json.Unmarshal(raw, &p))
	require.Equal(t, base, p)
	require.Error(t, json.Unmarshal([]byte(`{"unknown":1}`), &p))
}
func TestMetricsBoundedAndAccurateRates(t *testing.T) {
	p := testPolicy()
	p.MetricsSamples = 16
	c := controller(t, "", p)
	for i := 0; i < 100; i++ {
		c.Record(200, true, true, "", time.Millisecond, 10, 10)
	}
	s := c.Snapshot()
	require.Equal(t, int64(100), s.Totals.Attempts)
	require.Equal(t, int64(100), s.Latencies["attempt"].Count)
	require.Len(t, c.samples["attempt"], 16)
	require.Greater(t, s.Rates["attempts"], float64(0))
	require.Equal(t, int64(1000), s.Totals.Stored)
}

func TestZeroFreeDiskAndInitialHealthBlockBeforeFirstLease(t *testing.T) {
	p := testPolicy()
	c, err := New(context.Background(), t.Name(), p, func() Pressure { return Pressure{DiskKnown: true, DiskFreeMB: 0} })
	require.NoError(t, err)
	defer c.Close()
	require.Contains(t, c.Snapshot().Reason, "disk")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = c.Acquire(ctx)
	require.Error(t, err)
	c.Evaluate(time.Now(), Pressure{DiskKnown: true, DiskFreeMB: 1024})
	release, err := c.Acquire(context.Background())
	require.NoError(t, err)
	release()
}

func TestControllerCloseDropsIdleProcessAccounting(t *testing.T) {
	c, err := New(context.Background(), t.Name(), testPolicy(), nil)
	require.NoError(t, err)
	release, err := c.Acquire(context.Background())
	require.NoError(t, err)
	c.Close()
	release()
	global.mu.Lock()
	_, exists := global.active[t.Name()]
	global.mu.Unlock()
	require.False(t, exists)
}

func TestP95IncludesTailInSmallSamples(t *testing.T) {
	c := controller(t, "", testPolicy())
	c.Measure("attempt", time.Millisecond)
	c.Measure("attempt", 100*time.Millisecond)
	require.Equal(t, float64(100), c.Snapshot().Latencies["attempt"].P95)
}

func TestPolicyRejectsInconsistentLimitsAndNonFiniteThresholds(t *testing.T) {
	for name, mutate := range map[string]func(*Policy){
		"minimum":          func(p *Policy) { p.Minimum = p.Concurrency + 1 },
		"maximum":          func(p *Policy) { p.Maximum = p.Concurrency - 1 },
		"queue target":     func(p *Policy) { p.QueueTargetMultiplier = p.QueueLowMultiplier },
		"queue ceiling":    func(p *Policy) { p.QueueHardLimit = p.Concurrency - 1 },
		"refill":           func(p *Policy) { p.RefillBatch = p.QueueHardLimit + 1 },
		"candidate":        func(p *Policy) { p.CandidateWindow = p.RefillBatch - 1 },
		"adaptive ceiling": func(p *Policy) { p.Mode = "adaptive"; p.Maximum = p.QueueHardLimit + 1 },
		"downstream":       func(p *Policy) { p.DownstreamLow = p.DownstreamHigh },
		"memory":           func(p *Policy) { p.MemoryLowMB = p.MemoryHighMB },
		"cpu":              func(p *Policy) { p.CPULowPercent = p.CPUHighPercent },
		"host cooldown":    func(p *Policy) { p.HostCooldownMax = p.HostCooldown - 1 },
		"evaluation":       func(p *Policy) { p.AdjustmentInterval = p.SampleInterval - 1 },
		"nan":              func(p *Policy) { p.DecreaseFactor = math.NaN() },
		"infinity":         func(p *Policy) { p.OverloadFailureRatio = math.Inf(1) },
	} {
		t.Run(name, func(t *testing.T) { p := Default(); p.Mode = "adaptive"; mutate(&p); require.Error(t, p.Validate()) })
	}
	p := Default()
	require.Equal(t, runtime.NumCPU()*4, p.Concurrency)
	require.Greater(t, p.MemoryHighMB, p.MemoryLowMB)
	require.NoError(t, p.Validate())
	require.Error(t, json.Unmarshal([]byte(`{"mode":"fixed","concurrency":1.5}`), &p))
}

func TestFixedModeIgnoresOnlyInactiveAdaptiveControls(t *testing.T) {
	p := Default()
	p.Minimum = 999
	p.Maximum = 0
	p.AdjustmentInterval = 0
	p.MinSamples = 0
	p.IncreaseStep = 0
	p.CPULowPercent = 100
	p.CPUHighPercent = 0
	require.NoError(t, p.Validate())
	p.Mode = "adaptive"
	require.Error(t, p.Validate())
	p = Default()
	p.SampleInterval = 0
	require.Error(t, p.Validate())
}
func TestDispatchReportsEligibilityAndPolicySkips(t *testing.T) {
	c := controller(t, "", testPolicy())
	c.ReportDispatch(4096, 4096, 0, 0, false, "continuing candidate selection")
	s := c.Snapshot()
	require.False(t, s.Runnable)
	require.Equal(t, 4096, s.Dispatch.Inspected)
	require.EqualValues(t, 4096, s.Counters["selection_cooling"])
	c.ReportDispatch(10, 9, 1, 1, true, "requests published")
	require.True(t, c.Snapshot().Runnable)
	c.Record(0, false, false, "excluded", 0, 0, 0)
	s = c.Snapshot()
	require.Zero(t, s.Totals.Failures)
	require.Zero(t, s.Totals.Attempts)
	require.EqualValues(t, 1, s.Counters["policy_skips"])
}
