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
type Snapshot struct {
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
	finalSave                         sync.Once
	cancel                            context.CancelFunc
	done                              chan struct{}
	statePath                         string
	lastSave                          time.Time
	counters                          map[string]int64
	frontierPrevious                  map[string]int64
	frontierBuckets                   map[int64]map[string]int64
	queueHistory                      []queuePoint
	mu                                sync.Mutex
	policy                            Policy
	ctx                               context.Context
	key                               string
	effective                         int
	blocked                           bool
	reason, adjustment                string
	pressure                          Pressure
	pressureFn                        func() Pressure
	started, lastEval, lastAdjustment time.Time
	totals, previous                  Totals
	errors                            map[string]int64
	statuses                          map[int]int64
	samples                           map[string][]float64
	sampleAt                          map[string]int
	counts                            map[string]int64
	buckets                           map[int64]Totals
	smooth, baseline, lastRate        float64
}
type waiter struct {
	c       *Controller
	ready   chan struct{}
	granted bool
}
type broker struct {
	mu         sync.Mutex
	registered map[string]*Controller
	active     map[string]int
	used       int
	waiters    []*waiter
}

var global = broker{registered: map[string]*Controller{}, active: map[string]int{}}

func (b *broker) limit() int {
	limit := math.MaxInt
	for _, c := range b.registered {
		if c.policy.ProcessLimit < limit {
			limit = c.policy.ProcessLimit
		}
	}
	if limit == math.MaxInt {
		return 0
	}
	return limit
}
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
		close(w.ready)
	}
}
func New(ctx context.Context, key string, p Policy, pressure func() Pressure, statePath ...string) (*Controller, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	child, cancel := context.WithCancel(ctx)
	c := &Controller{ctx: child, cancel: cancel, done: make(chan struct{}), lastSave: time.Now(), key: key, policy: p, effective: p.Concurrency, started: time.Now(), lastEval: time.Now(), pressureFn: pressure, counters: map[string]int64{}, frontierPrevious: map[string]int64{}, frontierBuckets: map[int64]map[string]int64{}, errors: map[string]int64{}, statuses: map[int]int64{}, samples: map[string][]float64{}, sampleAt: map[string]int{}, counts: map[string]int64{}, buckets: map[int64]Totals{}, reason: "capacity available"}
	global.mu.Lock()
	if _, exists := global.registered[key]; exists {
		global.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("execution controller already registered")
	}
	global.registered[key] = c
	global.dispatch()
	global.mu.Unlock()
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
			global.mu.Lock()
			delete(global.registered, c.key)
			if global.active[c.key] == 0 {
				delete(global.active, c.key)
			}
			global.dispatch()
			global.mu.Unlock()
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
	global.mu.Lock()
	global.waiters = append(global.waiters, w)
	global.dispatch()
	global.mu.Unlock()
	select {
	case <-w.ready:
	case <-ctx.Done():
	case <-c.ctx.Done():
	}
	global.mu.Lock()
	granted := w.granted
	if !granted {
		for i, v := range global.waiters {
			if v == w {
				global.waiters = append(global.waiters[:i], global.waiters[i+1:]...)
				break
			}
		}
	}
	global.mu.Unlock()
	if !granted {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, c.ctx.Err()
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			global.mu.Lock()
			global.active[c.key]--
			global.used--
			if global.active[c.key] == 0 && global.registered[c.key] == nil {
				delete(global.active, c.key)
			}
			global.dispatch()
			global.mu.Unlock()
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
	if success {
		ms := float64(elapsed) / float64(time.Millisecond)
		if c.smooth == 0 {
			c.smooth = ms
			c.baseline = ms
		} else {
			c.smooth = c.policy.EWMAAlpha*ms + (1-c.policy.EWMAAlpha)*c.smooth
			if c.smooth < c.baseline {
				c.baseline = c.smooth
			}
		}
	}
}

// Evaluate uses only proxy/local failures for global overload; unreachable hosts
// are handled by the frontier's host cooldown policy.
func (c *Controller) Evaluate(now time.Time, p Pressure) {
	global.mu.Lock()
	waiting := false
	for _, w := range global.waiters {
		if w.c == c {
			waiting = true
			break
		}
	}
	global.mu.Unlock()
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
	reason := ""
	if p.Health != "" {
		reason = "storage/queue health: " + p.Health
	} else if (p.DiskKnown || p.DiskFreeMB > 0) && p.DiskFreeMB < float64(c.policy.MinFreeDiskMB) {
		reason = "disk space below configured minimum"
	} else if p.MemoryMB >= float64(c.policy.MemoryHighMB) || c.blocked && c.reason == "memory high watermark" && p.MemoryMB > float64(c.policy.MemoryLowMB) {
		reason = "memory high watermark"
	} else {
		for _, n := range p.Queues {
			if n >= int64(c.policy.DownstreamHigh) || c.blocked && c.reason == "downstream queue high watermark" && n > int64(c.policy.DownstreamLow) {
				reason = "downstream queue high watermark"
				break
			}
		}
	}
	c.blocked = reason != ""
	if reason != "" {
		c.reason = reason
	} else {
		c.reason = "capacity available"
	}
	if c.policy.Mode == "adaptive" && now.Sub(c.lastEval) >= c.policy.EvaluationWindow.Value() && now.Sub(c.lastAdjustment) >= c.policy.AdjustmentCooldown.Value() {
		completed := c.totals.Attempts - c.previous.Attempts
		if completed >= int64(c.policy.MinSamples) {
			seconds := now.Sub(c.lastEval).Seconds()
			rate := float64(c.totals.Successes-c.previous.Successes) / seconds
			overload := float64(c.totals.Overload-c.previous.Overload) / float64(completed)
			cpu := p.CPUPercent >= c.policy.CPUHighPercent
			inflated := c.baseline > 0 && c.smooth > c.baseline*c.policy.LatencyInflation
			next := c.effective
			why := ""
			if c.blocked || cpu || overload >= c.policy.OverloadFailureRatio || inflated {
				next = int(math.Floor(float64(next) * c.policy.DecreaseFactor))
				why = "reduce: configured pressure, proxy/local errors or latency inflation"
			} else if (p.FetchPending > 0 || waiting) && p.CPUPercent <= c.policy.CPULowPercent && (c.lastRate == 0 || rate >= c.lastRate*(1+c.policy.MinimumGain)) {
				next += c.policy.IncreaseStep
				why = "increase: healthy pressure and useful throughput"
			} else {
				why = "hold: throughput gain below configured threshold"
			}
			if next < c.policy.Minimum {
				next = c.policy.Minimum
			}
			if next > c.policy.Maximum {
				next = c.policy.Maximum
			}
			if next != c.effective {
				c.lastAdjustment = now
			}
			c.effective = next
			c.adjustment = why
			c.lastRate = rate
			c.previous = c.totals
			c.lastEval = now
		}
	}
	c.mu.Unlock()
	global.mu.Lock()
	global.dispatch()
	global.mu.Unlock()
}
func (c *Controller) Snapshot() Snapshot {
	c.mu.Lock()
	s := Snapshot{Mode: c.policy.Mode, Configured: c.policy.Concurrency, Effective: c.effective, Reason: c.reason, LastAdjustment: c.adjustment, Totals: c.totals, Pressure: copyPressure(c.pressure), Rates: map[string]float64{}, Errors: map[string]int64{}, HTTPStatuses: map[int]int64{}, Latencies: map[string]Histogram{}, Counters: map[string]int64{}, QueueGrowth: map[string]float64{}}
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
	global.mu.Lock()
	s.Active = global.active[c.key]
	s.ProcessActive = global.used
	s.ProcessLimit = global.limit()
	if s.Reason == "capacity available" && s.ProcessActive >= s.ProcessLimit {
		s.Reason = "process-wide request ceiling"
	}
	global.mu.Unlock()
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
	case "storage_conflicts", "storage_failures", "storage_operations":
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

// SharedOriginLimits is also used by legacy clients in this process.
func SharedOriginLimits() (int, time.Duration) {
	global.mu.Lock()
	defer global.mu.Unlock()
	if len(global.registered) == 0 {
		return 2, 100 * time.Millisecond
	}
	limit := math.MaxInt
	delay := time.Duration(0)
	for _, c := range global.registered {
		if c.policy.ProcessOriginLimit < limit {
			limit = c.policy.ProcessOriginLimit
		}
		if c.policy.ProcessOriginDelay.Value() > delay {
			delay = c.policy.ProcessOriginDelay.Value()
		}
	}
	return limit, delay
}

func (c *Controller) Close() { c.cancel(); <-c.done; c.finalSave.Do(c.save) }
