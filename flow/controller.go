package flow

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"sort"
	"sync"
	"time"
)

type Pressure struct {
	Frontier      map[string]int64   `json:"frontier"`
	FetchPending  int64              `json:"fetch_pending"`
	Queues        map[string]int64   `json:"queues"`
	OldestSeconds map[string]float64 `json:"oldest_seconds"`
	MemoryMB      float64            `json:"memory_mb"`
	CPUPercent    float64            `json:"cpu_percent"`
	DiskKnown     bool               `json:"disk_known"`
	DiskFreeMB    float64            `json:"disk_free_mb"`
	Health        string             `json:"health"`
}
type Histogram struct {
	Count   int64   `json:"count"`
	Mean    float64 `json:"mean_ms"`
	P50     float64 `json:"p50_ms"`
	P95     float64 `json:"p95_ms"`
	Maximum float64 `json:"max_ms"`
}
type Totals struct {
	Attempts  int64 `json:"attempts"`
	Successes int64 `json:"successes"`
	HTML      int64 `json:"html"`
	Bytes     int64 `json:"bytes_received"`
	Stored    int64 `json:"bytes_stored"`
	Failures  int64 `json:"failures"`
	Overload  int64 `json:"overload"`
}
type DispatchStatus struct {
	Reason    string `json:"reason"`
	Inspected int    `json:"inspected"`
	Cooling   int    `json:"cooling"`
	Eligible  int    `json:"eligible"`
	Published int    `json:"published"`
	Complete  bool   `json:"sweep_complete"`
}
type Snapshot struct {
	Dispatch            DispatchStatus `json:"dispatch"`
	ObservationAttempts int64          `json:"observation_attempts"`
	ObservationOverload int64          `json:"observation_overload"`
	NextAdjustment      time.Time      `json:"next_adjustment"`
	Runnable            bool           `json:"runnable_demand"`
	NetworkActive       int            `json:"network_active"`

	Counters       map[string]int64     `json:"counters"`
	QueueGrowth    map[string]float64   `json:"queue_growth_per_second"`
	Mode           string               `json:"mode"`
	Configured     int                  `json:"configured"`
	Effective      int                  `json:"effective"`
	Active         int                  `json:"active"`
	ProcessLimit   int                  `json:"process_limit"`
	ProcessActive  int                  `json:"process_active"`
	Reason         string               `json:"reason"`
	LastAdjustment string               `json:"last_adjustment"`
	Totals         Totals               `json:"totals"`
	Rates          map[string]float64   `json:"rates"`
	Errors         map[string]int64     `json:"errors"`
	HTTPStatuses   map[int]int64        `json:"http_statuses"`
	Latencies      map[string]Histogram `json:"latencies"`
	Pressure       Pressure             `json:"pressure"`
}
type Controller struct {
	dispatch                    DispatchStatus
	broker                      *broker
	queueBlocked, memoryBlocked bool
	runnable                    bool
	runnableAt                  time.Time
	saturated                   bool
	networkActive, networkPeak  int
	networkObserved             bool
	observationAttempts         int64
	observationOverload         int64
	nextAdjustment              time.Time

	finalSave          sync.Once
	cancel             context.CancelFunc
	done               chan struct{}
	statePath          string
	lastSave           time.Time
	counters           map[string]int64
	frontierPrevious   map[string]int64
	frontierBuckets    map[int64]map[string]int64
	queueHistory       []queuePoint
	mu                 sync.Mutex
	policy             Policy
	ctx                context.Context
	key                string
	effective          int
	blocked            bool
	reason, adjustment string
	pressure           Pressure
	pressureFn         func() Pressure
	started, lastEval  time.Time
	totals, previous   Totals
	errors             map[string]int64
	statuses           map[int]int64
	samples            map[string][]float64
	sampleAt           map[string]int
	counts             map[string]int64
	buckets            map[int64]Totals
}
type waiter struct {
	c       *Controller
	ready   chan struct{}
	granted bool
}
type broker struct {
	limits     ExecutionLimits
	mu         sync.Mutex
	registered map[string]*Controller
	active     map[string]int
	used       int
	waiters    []*waiter
}

var global = *NewBroker(DefaultExecutionLimits())

func (b *broker) limit() int { return b.limits.Concurrency }
func (b *broker) dispatch() {
	for b.used < b.limit() {
		at := -1
		for i, w := range b.waiters {
			if w.c.ctx.Err() != nil {
				continue
			}
			w.c.mu.Lock()
			n := w.c.effective
			if w.c.blocked {
				n = 0
			}
			w.c.mu.Unlock()
			if b.active[w.c.key] < n {
				at = i
				break
			}
		}
		if at < 0 {
			return
		}
		w := b.waiters[at]
		b.waiters = append(b.waiters[:at], b.waiters[at+1:]...)
		b.used++
		b.active[w.c.key]++
		w.granted = true
		w.c.mu.Lock()
		if b.active[w.c.key] >= w.c.effective {
			w.c.saturated = true
		}
		w.c.mu.Unlock()
		close(w.ready)
	}
}
func New(ctx context.Context, key string, p Policy, pressure func() Pressure, statePath ...string) (*Controller, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if err := brokerFrom(ctx).Limits().Validate(); err != nil {
		return nil, err
	}
	child, cancel := context.WithCancel(ctx)
	c := &Controller{broker: brokerFrom(ctx), ctx: child, cancel: cancel, done: make(chan struct{}), lastSave: time.Now(), key: key, policy: p, effective: p.Concurrency, started: time.Now(), lastEval: time.Now(), pressureFn: pressure, counters: map[string]int64{}, frontierPrevious: map[string]int64{}, frontierBuckets: map[int64]map[string]int64{}, errors: map[string]int64{}, statuses: map[int]int64{}, samples: map[string][]float64{}, sampleAt: map[string]int{}, counts: map[string]int64{}, buckets: map[int64]Totals{}, reason: "capacity available"}
	c.broker.mu.Lock()
	if _, exists := c.broker.registered[key]; exists {
		c.broker.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("execution controller already registered")
	}
	c.broker.registered[key] = c
	c.broker.dispatch()
	c.broker.mu.Unlock()
	if len(statePath) > 0 {
		c.statePath = statePath[0]
		c.restore()
	}
	if pressure != nil {
		c.Evaluate(time.Now(), pressure())
	}
	go c.run()
	return c, nil
}
func (c *Controller) run() {
	defer close(c.done)
	defer c.save()
	ticker := time.NewTicker(c.policy.SampleInterval.Value())
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			c.broker.mu.Lock()
			delete(c.broker.registered, c.key)
			if c.broker.active[c.key] == 0 {
				delete(c.broker.active, c.key)
			}
			c.broker.dispatch()
			c.broker.mu.Unlock()
			return
		case <-ticker.C:
			var p Pressure
			if c.pressureFn != nil {
				p = c.pressureFn()
			}
			c.Evaluate(time.Now(), p)
			if time.Since(c.lastSave) >= c.policy.MetricsSaveInterval.Value() {
				c.save()
				c.lastSave = time.Now()
			}
		}
	}
}
func (c *Controller) Acquire(ctx context.Context) (func(), error) {
	start := time.Now()
	w := &waiter{c: c, ready: make(chan struct{})}
	c.broker.mu.Lock()
	c.broker.waiters = append(c.broker.waiters, w)
	c.broker.dispatch()
	c.broker.mu.Unlock()
	select {
	case <-w.ready:
	case <-ctx.Done():
	case <-c.ctx.Done():
	}
	c.broker.mu.Lock()
	granted := w.granted
	if !granted {
		for i, v := range c.broker.waiters {
			if v == w {
				c.broker.waiters = append(c.broker.waiters[:i], c.broker.waiters[i+1:]...)
				break
			}
		}
	}
	c.broker.mu.Unlock()
	if !granted {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, c.ctx.Err()
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			c.broker.mu.Lock()
			c.broker.active[c.key]--
			c.broker.used--
			if c.broker.active[c.key] == 0 && c.broker.registered[c.key] == nil {
				delete(c.broker.active, c.key)
			}
			c.broker.dispatch()
			c.broker.mu.Unlock()
		})
	}
	if ctx.Err() != nil || c.ctx.Err() != nil {
		release()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, c.ctx.Err()
	}
	c.Measure("execution_wait", time.Since(start))
	return release, nil
}
func (c *Controller) Effective() int { c.mu.Lock(); defer c.mu.Unlock(); return c.effective }
func (c *Controller) DispatchBounds(current int) (int, int, int, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.blocked {
		return 0, 0, c.policy.CandidateWindow, c.reason
	}
	low := int(math.Ceil(float64(c.effective) * c.policy.QueueLowMultiplier))
	target := int(math.Ceil(float64(c.effective) * c.policy.QueueTargetMultiplier))
	if target > c.policy.QueueHardLimit {
		target = c.policy.QueueHardLimit
	}
	if current >= low || current >= target {
		return 0, 0, c.policy.CandidateWindow, "fetch queue above refill watermark"
	}
	n := target - current
	if n > c.policy.RefillBatch {
		n = c.policy.RefillBatch
	}
	return n, target, c.policy.CandidateWindow, "capacity available"
}
func (c *Controller) Measure(name string, d time.Duration) {
	switch name {
	case "attempt", "connect", "tls", "headers", "body", "execution_wait", "origin_wait", "storage", "queue_wait", "frontier_storage":
	default:
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[name]++
	values := c.samples[name]
	ms := float64(d) / float64(time.Millisecond)
	if len(values) < c.policy.MetricsSamples {
		c.samples[name] = append(values, ms)
	} else {
		at := c.sampleAt[name]
		values[at] = ms
		c.sampleAt[name] = (at + 1) % len(values)
	}
}
func add(a *Totals, b Totals) {
	a.Attempts += b.Attempts
	a.Successes += b.Successes
	a.HTML += b.HTML
	a.Bytes += b.Bytes
	a.Stored += b.Stored
	a.Failures += b.Failures
	a.Overload += b.Overload
}
func (c *Controller) Record(status int, success, html bool, kind string, elapsed time.Duration, received, stored int64) {
	if kind == "excluded" {
		c.mu.Lock()
		c.counters["policy_skips"]++
		c.mu.Unlock()
		return
	}
	c.Measure("attempt", elapsed)
	if status < 100 || status > 599 {
		status = 0
	}
	switch kind {
	case "timeout", "connection", "proxy", "policy", "http_429", "http_5xx", "http_4xx", "body", "storage", "cancelled", "":
	default:
		kind = "other"
	}
	v := Totals{Attempts: 1, Bytes: received, Stored: stored}
	if success {
		v.Successes = 1
		if html {
			v.HTML = 1
		}
	} else {
		v.Failures = 1
	}
	if kind == "proxy" || kind == "storage" {
		v.Overload = 1
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	add(&c.totals, v)
	if kind != "" {
		c.errors[kind]++
	}
	c.statuses[status]++
	width := c.policy.MetricsWindow.Value() / time.Duration(c.policy.MetricsSamples)
	if width < time.Millisecond {
		width = time.Millisecond
	}
	bucket := time.Now().UnixNano() / int64(width)
	b := c.buckets[bucket]
	add(&b, v)
	c.buckets[bucket] = b
	cut := bucket - int64(c.policy.MetricsSamples)
	for k := range c.buckets {
		if k < cut {
			delete(c.buckets, k)
		}
	}
}

// Evaluate uses only proxy/local failures for global overload; unreachable hosts
// are handled by the frontier's host cooldown policy.
func (c *Controller) Evaluate(now time.Time, p Pressure) {
	c.broker.mu.Lock()
	processFull := c.broker.used >= c.broker.limit()
	waiting := false
	for _, w := range c.broker.waiters {
		if w.c == c {
			waiting = true
			break
		}
	}
	c.broker.mu.Unlock()
	c.mu.Lock()
	p = copyPressure(p)
	c.pressure = p
	width := c.policy.MetricsWindow.Value() / time.Duration(c.policy.MetricsSamples)
	if width < time.Millisecond {
		width = time.Millisecond
	}
	bucket := now.UnixNano() / int64(width)
	if c.frontierBuckets[bucket] == nil {
		c.frontierBuckets[bucket] = map[string]int64{}
	}
	for _, key := range []string{"unique_html", "admitted", "terminal_outcomes", "retries_scheduled", "host_cooldowns"} {
		value := p.Frontier[key]
		if before, ok := c.frontierPrevious[key]; ok && value >= before {
			c.frontierBuckets[bucket][key] += value - before
		}
		c.frontierPrevious[key] = value
	}
	for k := range c.frontierBuckets {
		if k < bucket-int64(c.policy.MetricsSamples) {
			delete(c.frontierBuckets, k)
		}
	}
	c.queueHistory = append(c.queueHistory, queuePoint{at: now, depths: p.Queues})
	for len(c.queueHistory) > 1 && (len(c.queueHistory) > c.policy.MetricsSamples || now.Sub(c.queueHistory[0].at) > c.policy.MetricsWindow.Value()) {
		c.queueHistory = c.queueHistory[1:]
	}
	// Track each hysteresis gate independently; an overlapping gate cannot erase it.
	c.memoryBlocked = p.MemoryMB >= float64(c.policy.MemoryHighMB) || c.memoryBlocked && p.MemoryMB > float64(c.policy.MemoryLowMB)
	queueBlocked := false
	for _, n := range p.Queues {
		if n >= int64(c.policy.DownstreamHigh) || c.queueBlocked && n > int64(c.policy.DownstreamLow) {
			queueBlocked = true
		}
	}
	c.queueBlocked = queueBlocked
	reason := ""
	switch {
	case p.Health != "":
		reason = "storage_health: " + p.Health
	case (p.DiskKnown || p.DiskFreeMB > 0) && p.DiskFreeMB < float64(c.policy.MinFreeDiskMB):
		reason = "disk_low"
	case c.memoryBlocked:
		reason = "memory_high"
	case c.queueBlocked:
		reason = "downstream_high"
	}
	c.blocked = reason != ""
	c.reason = reason
	if !c.blocked {
		c.reason = "capacity available"
	}
	c.nextAdjustment = c.lastEval.Add(c.policy.AdjustmentInterval.Value())
	if now.Before(c.nextAdjustment) {
		c.mu.Unlock()
		c.broker.mu.Lock()
		c.broker.dispatch()
		c.broker.mu.Unlock()
		return
	}
	completed := c.totals.Attempts - c.previous.Attempts
	overloadCount := c.totals.Overload - c.previous.Overload
	c.observationAttempts = completed
	c.observationOverload = overloadCount
	// Advance even without completions, so old errors and idle windows expire.
	c.previous = c.totals
	c.lastEval = now
	c.nextAdjustment = now.Add(c.policy.AdjustmentInterval.Value())
	runnable := p.FetchPending > 0 || waiting || c.runnable && now.Sub(c.runnableAt) <= 2*c.policy.SampleInterval.Value()
	saturated := c.networkPeak >= c.effective || !c.networkObserved && c.saturated
	next := c.effective
	if c.policy.Mode == "adaptive" {
		switch {
		case c.blocked:
			c.adjustment = "hold: " + reason
		case p.CPUPercent >= c.policy.CPUHighPercent:
			next = int(math.Floor(float64(next) * c.policy.DecreaseFactor))
			c.adjustment = "reduce: cpu_high"
		case completed >= int64(c.policy.MinSamples) && float64(overloadCount)/float64(completed) >= c.policy.OverloadFailureRatio:
			next = int(math.Floor(float64(next) * c.policy.DecreaseFactor))
			c.adjustment = "reduce: local_overload"
		case !runnable:
			c.adjustment = "hold: no_runnable_work"
		case !saturated:
			c.adjustment = "hold: execution_not_saturated"
		case processFull:
			c.adjustment = "hold: process_limit"
		case p.CPUPercent > c.policy.CPULowPercent:
			c.adjustment = "hold: cpu_above_resume"
		default:
			next += c.policy.IncreaseStep
			c.adjustment = "increase: runnable_demand"
		}
		if next < c.policy.Minimum {
			next = c.policy.Minimum
		}
		if next > c.policy.Maximum {
			next = c.policy.Maximum
		}
		if next == c.effective && next == c.policy.Maximum && !c.blocked {
			c.adjustment = "hold: maximum_concurrency"
		}
		c.effective = next
	}
	c.saturated = false
	c.networkPeak = c.networkActive
	c.mu.Unlock()
	c.broker.mu.Lock()
	c.broker.dispatch()
	c.broker.mu.Unlock()
}
func (c *Controller) Snapshot() Snapshot {
	c.mu.Lock()
	s := Snapshot{Dispatch: c.dispatch, ObservationAttempts: c.observationAttempts, ObservationOverload: c.observationOverload, NextAdjustment: c.nextAdjustment, Runnable: c.pressure.FetchPending > 0 || c.runnable && time.Since(c.runnableAt) <= 2*c.policy.SampleInterval.Value(), NetworkActive: c.networkActive, Mode: c.policy.Mode, Configured: c.policy.Concurrency, Effective: c.effective, Reason: c.reason, LastAdjustment: c.adjustment, Totals: c.totals, Pressure: copyPressure(c.pressure), Rates: map[string]float64{}, Errors: map[string]int64{}, HTTPStatuses: map[int]int64{}, Latencies: map[string]Histogram{}, Counters: map[string]int64{}, QueueGrowth: map[string]float64{}}
	for k, v := range c.counters {
		s.Counters[k] = v
	}
	if len(c.queueHistory) > 1 {
		first, last := c.queueHistory[0], c.queueHistory[len(c.queueHistory)-1]
		seconds := last.at.Sub(first.at).Seconds()
		if seconds > 0 {
			for key, n := range last.depths {
				s.QueueGrowth[key] = float64(n-first.depths[key]) / seconds
			}
		}
	}
	for k, v := range c.errors {
		s.Errors[k] = v
	}
	for k, v := range c.statuses {
		s.HTTPStatuses[k] = v
	}
	for k, values := range c.samples {
		copy := append([]float64{}, values...)
		sort.Float64s(copy)
		if len(copy) == 0 {
			continue
		}
		sum := 0.0
		for _, v := range copy {
			sum += v
		}
		s.Latencies[k] = Histogram{Count: c.counts[k], Mean: sum / float64(len(copy)), P50: copy[(len(copy)-1)/2], P95: copy[int(math.Ceil(float64(len(copy))*.95))-1], Maximum: copy[len(copy)-1]}
	}
	width := c.policy.MetricsWindow.Value() / time.Duration(c.policy.MetricsSamples)
	if width < time.Millisecond {
		width = time.Millisecond
	}
	cut := time.Now().Add(-c.policy.MetricsWindow.Value()).UnixNano() / int64(width)
	t := Totals{}
	for k, v := range c.buckets {
		if k >= cut {
			add(&t, v)
		}
	}
	seconds := math.Min(time.Since(c.started).Seconds(), c.policy.MetricsWindow.Value().Seconds())
	if seconds > 0 {
		s.Rates["attempts"] = float64(t.Attempts) / seconds
		s.Rates["successes"] = float64(t.Successes) / seconds
		s.Rates["html"] = float64(t.HTML) / seconds
		s.Rates["failures"] = float64(t.Failures) / seconds
		s.Rates["bytes"] = float64(t.Bytes) / seconds
		for key, values := range c.frontierBuckets {
			if key >= cut {
				for name, n := range values {
					s.Rates[name] += float64(n) / seconds
				}
			}
		}
	}
	c.mu.Unlock()
	c.broker.mu.Lock()
	s.Active = c.broker.active[c.key]
	s.ProcessActive = c.broker.used
	s.ProcessLimit = c.broker.limit()
	for _, waiter := range c.broker.waiters {
		if waiter.c == c {
			s.Runnable = true
			break
		}
	}
	if s.Reason == "capacity available" && s.ProcessActive >= s.ProcessLimit {
		s.Reason = "process-wide request ceiling"
	}
	c.broker.mu.Unlock()
	return s
}
func HeapMB() float64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.Alloc) / (1 << 20)
}

type queuePoint struct {
	at     time.Time
	depths map[string]int64
}

func (c *Controller) Counter(key string, n int64) {
	switch key {
	case "storage_conflicts", "storage_failures", "storage_operations", "feed_policy_skips":
	default:
		return
	}
	c.mu.Lock()
	c.counters[key] += n
	c.mu.Unlock()
}

func copyPressure(p Pressure) Pressure {
	q := p
	q.Queues = map[string]int64{}
	q.OldestSeconds = map[string]float64{}
	q.Frontier = map[string]int64{}
	for k, v := range p.Queues {
		q.Queues[k] = v
	}
	for k, v := range p.OldestSeconds {
		q.OldestSeconds[k] = v
	}
	for k, v := range p.Frontier {
		q.Frontier[k] = v
	}
	return q
}

// ReportRunnable reuses the publisher's bounded candidate pass.
func (c *Controller) ReportRunnable(value bool) {
	c.mu.Lock()
	c.runnable = value
	c.runnableAt = time.Now()
	c.mu.Unlock()
}
func (c *Controller) SharedOriginLimits() (int, time.Duration) {
	v := c.broker.Limits()
	return v.HostConcurrency, v.HostDelay.Value()
}
func SharedOriginLimits() (int, time.Duration) {
	v := global.Limits()
	return v.HostConcurrency, v.HostDelay.Value()
}

type fetchAttemptKey struct{}

func FetchAttempt(ctx context.Context) context.Context {
	return context.WithValue(ctx, fetchAttemptKey{}, true)
}
func (c *Controller) BeginNetwork(ctx context.Context) func() {
	if ctx.Value(fetchAttemptKey{}) != true {
		return func() {}
	}
	c.mu.Lock()
	c.networkObserved = true
	c.networkActive++
	if c.networkActive > c.networkPeak {
		c.networkPeak = c.networkActive
	}
	c.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { c.mu.Lock(); c.networkActive--; c.mu.Unlock() }) }
}

func (c *Controller) Close() { c.cancel(); <-c.done; c.finalSave.Do(c.save) }

// ReportDispatch reports bounded selection activity, not candidate existence.
func (c *Controller) ReportDispatch(inspected, cooling, eligible, published int, complete bool, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.runnable = eligible > 0
	c.runnableAt = time.Now()
	c.dispatch = DispatchStatus{reason, inspected, cooling, eligible, published, complete}
	for k, v := range map[string]int{"selection_inspected": inspected, "selection_cooling": cooling, "selection_eligible": eligible, "selection_published": published} {
		c.counters[k] += int64(v)
	}
	if complete {
		c.counters["selection_sweeps"]++
	}
}
