package flow

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func recoveryController(t *testing.T) *Controller {
	p := Default()
	p.Mode = "adaptive"
	p.Concurrency = 32
	p.Maximum = 64
	p.IncreaseStep = 8
	p.SampleInterval = Duration(time.Second)
	p.AdjustmentInterval = Duration(30 * time.Second)
	c, err := New(WithBroker(context.Background(), NewBroker(DefaultExecutionLimits())), t.Name(), p, nil)
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c
}
func TestRecoveryFromMinimumDoesNotRequireCompletionsOrSuccess(t *testing.T) {
	c := recoveryController(t)
	c.mu.Lock()
	c.effective = 1
	now := c.lastEval
	c.mu.Unlock()
	for _, target := range []int{9, 17, 25, 33} {
		c.mu.Lock()
		c.saturated = true
		c.mu.Unlock()
		now = now.Add(30 * time.Second)
		c.Evaluate(now, Pressure{FetchPending: 100, DiskKnown: true, DiskFreeMB: 1024})
		require.Equal(t, target, c.Effective())
	}
}
func TestRemoteLatencyAndDestinationFailuresCannotReduceCapacity(t *testing.T) {
	c := recoveryController(t)
	now := c.lastEval
	for i := 0; i < 50; i++ {
		c.Record(0, false, false, "timeout", time.Minute, 0, 0)
	}
	c.Record(200, true, true, "", time.Millisecond, 1, 1)
	c.Record(200, true, true, "", time.Minute, 1, 1)
	c.Evaluate(now.Add(30*time.Second), Pressure{FetchPending: 100})
	require.Equal(t, 32, c.Effective())
	require.Zero(t, c.Snapshot().Totals.Overload)
}
func TestBlockedQueuesDoNotCompoundReductionsAndRecoveryUsesFreshErrors(t *testing.T) {
	c := recoveryController(t)
	now := c.lastEval
	for i := 0; i < 20; i++ {
		c.Record(0, false, false, "proxy", time.Millisecond, 0, 0)
	}
	for i := 0; i < 8; i++ {
		now = now.Add(30 * time.Second)
		c.Evaluate(now, Pressure{Queues: map[string]int64{"analyzed": 128}, FetchPending: 100})
	}
	require.Equal(t, 32, c.Effective())
	c.mu.Lock()
	c.saturated = true
	c.mu.Unlock()
	c.Evaluate(now.Add(30*time.Second), Pressure{Queues: map[string]int64{"analyzed": 0}, FetchPending: 100})
	require.Equal(t, 40, c.Effective())
	require.Zero(t, c.Snapshot().ObservationOverload)
}
func TestOverlappingPressureGatesHaveIndependentHysteresis(t *testing.T) {
	c := recoveryController(t)
	now := time.Now()
	c.Evaluate(now, Pressure{MemoryMB: float64(c.policy.MemoryHighMB), Queues: map[string]int64{"analyzed": 128}})
	c.Evaluate(now.Add(time.Second), Pressure{MemoryMB: 0, Queues: map[string]int64{"analyzed": 100}})
	require.Equal(t, "downstream_high", c.Snapshot().Reason)
	c.Evaluate(now.Add(2*time.Second), Pressure{Queues: map[string]int64{"analyzed": 0}})
	require.Equal(t, "capacity available", c.Snapshot().Reason)
	require.Equal(t, 32, c.Effective())
}
func TestCPUAndConfirmedLocalOverloadReduceWithoutReplayingOldWindows(t *testing.T) {
	c := recoveryController(t)
	now := c.lastEval.Add(30 * time.Second)
	c.Evaluate(now, Pressure{CPUPercent: 95, FetchPending: 100})
	require.Equal(t, 25, c.Effective())
	for i := 0; i < 20; i++ {
		c.Record(0, false, false, "proxy", time.Millisecond, 0, 0)
	}
	now = now.Add(30 * time.Second)
	c.Evaluate(now, Pressure{FetchPending: 100})
	require.Equal(t, 20, c.Effective())
	now = now.Add(30 * time.Second)
	c.Evaluate(now, Pressure{FetchPending: 100})
	require.Equal(t, 20, c.Effective())
	require.Zero(t, c.Snapshot().ObservationOverload)
	c.mu.Lock()
	c.saturated = true
	c.mu.Unlock()
	c.Evaluate(now.Add(30*time.Second), Pressure{FetchPending: 100})
	require.Equal(t, 28, c.Effective())
}
func TestNetworkOriginWaitIsNotExecutionSaturation(t *testing.T) {
	c := recoveryController(t)
	c.saturated = true
	c.networkObserved = true
	c.networkPeak = 2
	c.Evaluate(c.lastEval.Add(30*time.Second), Pressure{FetchPending: 100})
	require.Equal(t, 32, c.Effective())
	require.Equal(t, "hold: execution_not_saturated", c.Snapshot().LastAdjustment)
}
func TestLegacyAdaptivePolicyMigratesLongestIntervalAndDropsRetiredFields(t *testing.T) {
	raw, _ := json.Marshal(Default())
	var value map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &value))
	delete(value, "adjustment_interval")
	value["evaluation_window"] = "15s"
	value["adjustment_cooldown"] = "45s"
	value["latency_inflation"] = 1.5
	value["minimum_gain"] = 0.02
	value["process_limit"] = 10
	raw, _ = json.Marshal(value)
	var p Policy
	require.NoError(t, json.Unmarshal(raw, &p))
	require.Equal(t, Duration(45*time.Second), p.AdjustmentInterval)
	raw, _ = json.Marshal(p)
	for _, key := range []string{"latency_inflation", "minimum_gain", "process_limit", "evaluation_window", "adjustment_cooldown"} {
		require.NotContains(t, string(raw), key)
	}
}
func TestWorkspaceLimitReductionDrainsExistingPermits(t *testing.T) {
	broker := NewBroker(ExecutionLimits{Concurrency: 2, HostConcurrency: 2})
	p := Default()
	p.Concurrency = 2
	p.Maximum = 4
	c, err := New(WithBroker(context.Background(), broker), t.Name(), p, nil)
	require.NoError(t, err)
	defer c.Close()
	a, err := c.Acquire(context.Background())
	require.NoError(t, err)
	b, err := c.Acquire(context.Background())
	require.NoError(t, err)
	require.NoError(t, broker.SetLimits(ExecutionLimits{Concurrency: 1, HostConcurrency: 2}))
	require.Equal(t, 2, c.Snapshot().ProcessActive)
	a()
	require.Equal(t, 1, c.Snapshot().ProcessActive)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = c.Acquire(ctx)
	require.Error(t, err)
	b()
	release, err := c.Acquire(context.Background())
	require.NoError(t, err)
	release()
	require.Zero(t, c.Snapshot().ProcessActive)
}
func TestInvalidExecutionLimitsFailBeforeRegisteringController(t *testing.T) {
	p := Default()
	_, err := New(WithBroker(context.Background(), NewBroker(ExecutionLimits{})), t.Name(), p, nil)
	require.Error(t, err)
}

func TestRecoveryTimerGrowsWithRunnableDemandAndNoCompletedSamples(t *testing.T) {
	p := Default()
	p.Mode = "adaptive"
	p.Concurrency = 1
	p.Minimum = 1
	p.Maximum = 3
	p.IncreaseStep = 1
	p.SampleInterval = Duration(25 * time.Millisecond)
	p.AdjustmentInterval = Duration(250 * time.Millisecond)
	c, err := New(WithBroker(context.Background(), NewBroker(DefaultExecutionLimits())), t.Name(), p, func() Pressure { return Pressure{FetchPending: 10} })
	require.NoError(t, err)
	defer c.Close()
	for target := 2; target <= 3; target++ {
		release, err := c.Acquire(context.Background())
		require.NoError(t, err)
		defer release()
		end := c.BeginNetwork(FetchAttempt(context.Background()))
		defer end()
		// Evaluate's fake-clock tests assert the exact interval boundary. This
		// real-clock check permits one sample plus scheduler/race-detector jitter.
		require.Eventually(t, func() bool { return c.Effective() >= target }, 350*time.Millisecond, 5*time.Millisecond)
	}
	require.Zero(t, c.Snapshot().Totals.Attempts)
}
