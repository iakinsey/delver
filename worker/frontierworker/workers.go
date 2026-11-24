// Package frontierworker implements frontier orchestration using ordinary workers.
package frontierworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/armon/go-metrics"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/instrument"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/repetition"
	"github.com/iakinsey/delver/report"
	"github.com/iakinsey/delver/trap"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"math"
	"net/url"
	"regexp"

	"time"
)

type Params struct {
	Flow                *flow.Controller       `json:"-"`
	RetryShare          float64                `json:"retry_share"`
	AllowedHostSuffixes []string               `json:"allowed_host_suffixes,omitempty"`
	AllowPattern        string                 `json:"allow_pattern"`
	DenyPattern         string                 `json:"deny_pattern"`
	Store               frontier.FrontierStore `json:"-" resource:"frontier_store"`
	Output              queue.Queue            `json:"-" resource:"output_queue"`
	Strategy            string                 `json:"strategy"`
	Scope               string                 `json:"scope"`
	MaxHops             int                    `json:"max_hops"`
	MaxDepth            int                    `json:"max_depth"`
	BatchSize           int                    `json:"batch_size"`
	LowWater            int                    `json:"low_water"`
	Window              int                    `json:"window"`
}
type Worker struct {
	policy   *protection.Policy
	Reporter report.Writer
	metrics  metrics.MetricSink
	Params
	Kind         string
	lastHost     string
	step         uint64
	repeatedStep uint64
	Selector     frontier.Selector
	allow, deny  *regexp.Regexp
	scanCursor   frontier.ScanCursor
	nextCleanup  time.Time
	Filters      []frontier.Filter
}

func (w *Worker) SetProtection(p *protection.Policy) { w.policy = p }
func (w *Worker) SetReporter(r report.Writer)        { w.Reporter = r }

func New(p Params, kind string) (*Worker, error) {
	suffixes, err := util.NormalizeHostSuffixes(p.AllowedHostSuffixes)
	if err != nil {
		return nil, err
	}
	p.AllowedHostSuffixes = suffixes

	for _, pattern := range []string{p.AllowPattern, p.DenyPattern} {
		if pattern != "" {
			if _, err := regexp.Compile(pattern); err != nil {
				return nil, err
			}
		}
	}
	if p.Store == nil {
		return nil, fmt.Errorf("frontier_store required")
	}
	if p.Strategy == "" {
		p.Strategy = "host_diverse"
	}
	switch p.Strategy {
	case "breadth_first", "depth_first", "host_diverse", "adaptive":
	default:
		return nil, fmt.Errorf("unsupported strategy %s", p.Strategy)
	}
	if p.Scope == "" {
		p.Scope = "same_domain"
	}
	switch p.Scope {
	case "same_origin", "same_domain", "open_web":
	default:
		return nil, fmt.Errorf("unsupported scope")
	}
	if p.MaxHops == 0 {
		p.MaxHops = 3
	}
	if p.MaxDepth == 0 {
		p.MaxDepth = 10
	}
	if p.BatchSize == 0 {
		p.BatchSize = 32
	}
	if p.LowWater == 0 {
		p.LowWater = 64
	}
	if p.Window == 0 {
		p.Window = 1024
	}
	if p.MaxDepth < 0 || p.MaxHops < 0 || p.BatchSize < 1 || p.LowWater < 1 || p.Window < p.BatchSize {
		return nil, fmt.Errorf("invalid frontier limits")
	}
	if kind == "publisher" && p.Output == nil {
		return nil, fmt.Errorf("output_queue required")
	}
	w := &Worker{Params: p, Kind: kind, metrics: instrument.GetMetrics()}
	if p.AllowPattern != "" {
		w.allow = regexp.MustCompile(p.AllowPattern)
	}
	if p.DenyPattern != "" {
		w.deny = regexp.MustCompile(p.DenyPattern)
	}
	return w, nil
}
func (w *Worker) OnComplete() {}
func (w *Worker) OnMessage(m types.Message) (interface{}, error) {
	return w.OnMessageContext(context.Background(), m)
}
func (w *Worker) OnMessageContext(ctx context.Context, m types.Message) (interface{}, error) {
	if w.Kind == "cleanup" {
		now := time.Now()
		if now.Before(w.nextCleanup) {
			return nil, nil
		}
		_, err := w.Store.Cleanup(ctx, now)
		if err == nil {
			w.nextCleanup = now.Add(w.Store.CleanupInterval())
		}
		return nil, err
	}
	if w.Kind == "publisher" {
		return nil, w.publish(ctx)
	}
	var c message.CompositeAnalysis
	if err := json.Unmarshal(m.Message, &c); err != nil {
		return nil, err
	}
	var links features.URIs
	if c.Success && c.Has(features.UrlField) {
		if err := c.Load(features.UrlField, &links); err != nil {
			return nil, err
		}
	}
	base := c.EffectiveURI
	if base == "" {
		base = c.URI
	}
	base, err := frontier.Normalize(base, "")
	if err != nil {
		return nil, err
	}
	source, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	if target, ok := w.Store.(interface {
		OutcomeAnalysis(context.Context, string, message.CompositeAnalysis, []string) error
	}); ok {
		if err := target.OutcomeAnalysis(ctx, m.ID, c, links); err != nil {
			return nil, err
		}
	} else if err := w.Store.Outcome(c.FetcherResponse); err != nil {
		return nil, err
	}
	if target, ok := w.Store.(interface {
		FlushRepeated(context.Context, repetition.Writer) error
	}); ok {
		if reporter, ok := w.Reporter.(repetition.Writer); ok {
			if err := target.FlushRepeated(ctx, reporter); err != nil {
				return nil, err
			}
		}
	}
	gatewayBlocked := false
	if target, ok := w.Store.(interface {
		ObserveGateway(context.Context, message.CompositeAnalysis, features.URIs) (bool, error)
	}); ok {
		gatewayBlocked, err = target.ObserveGateway(ctx, c, links)
		if err != nil {
			return nil, err
		}
	}
	var adversarial features.Adversarial
	if c.LoadPermissive(features.AdversarialField, &adversarial) && adversarial.Structure != nil {
		if target, ok := w.Store.(interface {
			ObserveTrap(context.Context, trap.Observation) (bool, error)
		}); ok {
			_, e := target.ObserveTrap(ctx, *adversarial.Structure)
			if e != nil {
				return nil, e
			}
			// The persisted site exclusions filter staging. Independent outward
			// destinations remain eligible after this page supplies decisive evidence.
		}
	}
	var discoveries []message.FetcherRequest
	for _, link := range links {
		if e := w.policy.Check(link); e != nil {
			if errors.Is(e, protection.ErrBlocked) {
				continue
			}
			return nil, e
		}
		uri, err := frontier.Normalize(link, base)
		if err != nil {
			continue
		}
		if !util.URLHostnameAllowed(uri, w.AllowedHostSuffixes) {
			continue
		}
		if w.allow != nil && !w.allow.MatchString(uri) {
			continue
		}
		if w.deny != nil && w.deny.MatchString(uri) {
			continue
		}
		allowed := true
		for _, filter := range w.Filters {
			ok, err := filter.IsAllowed(uri)
			if err != nil {
				return nil, err
			}
			if !ok {
				allowed = false
				break
			}
		}
		if !allowed {
			continue
		}
		target, _ := url.Parse(uri)
		if w.Scope == "same_origin" && (target.Scheme != source.Scheme || target.Host != source.Host) {
			continue
		}
		if w.Scope == "same_domain" && domain(target.Hostname()) != domain(source.Hostname()) {
			continue
		}
		depth, hops := c.Depth+1, c.Hops
		if domain(target.Hostname()) != domain(source.Hostname()) {
			depth = 0
			hops++
		}
		if depth > w.MaxDepth || hops > w.MaxHops {
			continue
		}
		discoveries = append(discoveries, message.FetcherRequest{URI: uri, Origin: c.URI, Depth: depth, Hops: hops, ScopeRoot: c.ScopeRoot})
	}
	if w.Reporter != nil {
		records := []report.URL{}
		for _, r := range discoveries {
			if !util.URLFileExcluded(r.URI, admissionExtensions(w.Store)) {
				records = append(records, report.URL{URI: r.URI, Origin: base})
			}
		}
		if err := w.Reporter.Discover(ctx, records); err != nil {
			return nil, err
		}
	}
	if gatewayBlocked {
		discoveries = nil
	}
	id := fmt.Sprintf("%s:%s:%d:%d:%s", c.RequestID, c.URI, c.Depth, c.Hops, m.ID)
	if err := w.Store.StageDiscovery(ctx, id, discoveries); err != nil {
		return nil, err
	}
	_, err = w.Store.DrainDiscovery(ctx, w.BatchSize)
	return nil, err
}
func domain(h string) string { return util.SiteKey(h) }
func (w *Worker) publish(ctx context.Context) error {
	if source, ok := w.Store.(interface{ RepeatedDispatchStep() (uint64, error) }); ok {
		step, err := source.RepeatedDispatchStep()
		if err != nil {
			return err
		}
		w.repeatedStep = step
	}
	if target, ok := w.Store.(interface {
		MaintainRepeatedRouting(context.Context, int) error
	}); ok {
		if err := target.MaintainRepeatedRouting(ctx, 128); err != nil {
			return err
		}
	}
	if target, ok := w.Store.(interface {
		FlushRepeated(context.Context, repetition.Writer) error
	}); ok {
		if reporter, ok := w.Reporter.(repetition.Writer); ok {
			if err := target.FlushRepeated(ctx, reporter); err != nil {
				return err
			}
		}
	}

	if target, ok := w.Store.(interface {
		DrainParked(context.Context, int) error
	}); ok {
		if e := target.DrainParked(ctx, w.BatchSize); e != nil {
			return e
		}
	}
	if target, ok := w.Store.(interface {
		DrainRetries(context.Context, int) error
	}); ok {
		if err := target.DrainRetries(ctx, w.Window); err != nil {
			return err
		}
	}
	if target, ok := w.Store.(interface {
		DrainRecrawls(context.Context, int) error
	}); ok {
		if err := target.DrainRecrawls(ctx, w.Window); err != nil {
			return err
		}
	}
	if _, err := w.Store.DrainDiscovery(ctx, w.BatchSize); err != nil {
		return err
	}
	n := w.Output.Len()
	if n < 0 {
		return fmt.Errorf("fetch queue unavailable")
	}
	remaining := w.LowWater - int(n)
	dispatchReason := "dispatch capacity blocked"
	if w.Flow != nil {
		var window int
		remaining, _, window, dispatchReason = w.Flow.DispatchBounds(int(n))
		w.Window = window
	}
	if remaining <= 0 {
		if w.Flow != nil {
			w.Flow.ReportDispatch(0, 0, 0, 0, false, dispatchReason)
		}
		return nil
	}
	if w.Flow == nil && remaining > w.BatchSize {
		remaining = w.BatchSize
	}
	var candidates []frontier.Candidate
	var err error
	inspected, cooling := 0, 0
	complete := true
	if scanner, ok := w.Store.(interface {
		ScanCandidates(context.Context, string, frontier.ScanCursor, int) (frontier.CandidatePage, error)
	}); ok {
		page, e := scanner.ScanCandidates(ctx, w.Strategy, w.scanCursor, w.Window)
		err = e
		if err == nil {
			if target, ok := w.Store.(interface {
				ParkTraps(context.Context, []frontier.Candidate) error
			}); ok {
				if e := target.ParkTraps(ctx, page.Trapped); e != nil {
					return e
				}
			}
			for _, excluded := range page.Excluded {
				if e := w.Store.Outcome(message.FetcherResponse{FetcherRequest: excluded.Request, FailureClass: "excluded", Error: "file extension excluded by crawl policy"}); e != nil {
					return e
				}
				if w.Flow != nil {
					w.Flow.Record(0, false, false, "excluded", 0, 0, 0)
				}
			}
		}
		candidates = page.Candidates
		w.scanCursor = page.Cursor
		inspected = page.Inspected
		cooling = page.Cooling
		complete = page.Complete
	} else {
		candidates, err = w.Store.Candidates(ctx, w.Strategy, w.lastHost, w.Window)
		inspected = len(candidates)
	}
	if err != nil {
		return err
	}
	filtered := candidates[:0]
	for _, c := range candidates {
		if e := w.policy.Check(c.Request.URI); e != nil {
			if errors.Is(e, protection.ErrBlocked) {
				continue
			}
			return e
		}
		filtered = append(filtered, c)
	}
	candidates = filtered
	eligible, published := len(candidates), 0
	reason := "no pending work"
	defer func() {
		if complete || remaining == 0 {
			w.scanCursor = frontier.ScanCursor{}
		}
		if w.Flow != nil {
			w.Flow.ReportDispatch(inspected, cooling, eligible, published, complete, reason)
		}
	}()
	if !complete {
		reason = "continuing candidate selection"
	} else if cooling > 0 {
		reason = "host cooldown"
	}
	var scores map[string]float64
	if w.Strategy == "adaptive" {
		scores, err = w.Store.Scores(candidates)
		if err != nil {
			return err
		}
	}
	for len(candidates) > 0 && remaining > 0 {
		selector := w.Selector
		if selector == nil {
			selector = frontier.Strategy(w.Strategy)
		}
		selection := candidates
		positions := []int{}
		if w.RetryShare > 0 {
			retries := retryTurn(w.step, w.RetryShare)
			subset := []frontier.Candidate{}
			for i, c := range candidates {
				if (c.Request.Attempt > 0) == retries {
					subset = append(subset, c)
					positions = append(positions, i)
				}
			}
			if len(subset) > 0 {
				selection = subset
			}
		}
		if policy, ok := w.Store.(interface{ RepeatedAdmissionShare() int }); ok && policy.RepeatedAdmissionShare() > 0 {
			share := policy.RepeatedAdmissionShare()
			chooseRepeated := repetition.Turn(w.repeatedStep, share)
			freshOnly := true
			for _, entry := range selection {
				if entry.Request.Attempt > 0 || entry.State == "reserved" {
					freshOnly = false
					break
				}
			}
			if freshOnly {
				subset := []frontier.Candidate{}
				mapped := []int{}
				for i, entry := range selection {
					if entry.Repeated == chooseRepeated {
						subset = append(subset, entry)
						p := i
						if len(positions) > 0 {
							p = positions[i]
						}
						mapped = append(mapped, p)
					}
				}
				if len(subset) > 0 {
					selection = subset
					positions = mapped
				}
			}
		}
		at := selector.Select(selection, w.lastHost, w.step, scores)
		if at < 0 || at >= len(selection) {
			return fmt.Errorf("selector returned invalid candidate index %d", at)
		}
		if len(positions) > 0 {
			at = positions[at]
		}

		c := candidates[at]
		freshRepeatedTurn := c.Request.Attempt == 0
		candidates = append(candidates[:at], candidates[at+1:]...)
		c, err = w.Store.Reserve(ctx, c)
		if errors.Is(err, frontier.ErrTrapExcluded) {
			if target, ok := w.Store.(interface {
				ParkTraps(context.Context, []frontier.Candidate) error
			}); ok {
				if err := target.ParkTraps(ctx, []frontier.Candidate{c}); err != nil {
					return err
				}
			}
			eligible--
			continue
		}
		if errors.Is(err, frontier.ErrDispatchBudget) {
			reason = "request budget reached"
			eligible = 0
			break
		}
		if errors.Is(err, frontier.ErrCooling) {
			cooling++
			eligible--
			reason = "host cooldown"
			continue
		}
		if err != nil {
			return err
		}
		c.Request.QueuedAt = time.Now().UnixNano()
		body, err := json.Marshal(c.Request)
		if err != nil {
			return err
		}
		if err := queue.PutContext(ctx, w.Output, types.Message{ID: string(c.Request.RequestID), MessageType: types.FetcherRequestType, Message: body}, 0); err != nil {
			return err
		}
		if err := w.Store.Published(c); err != nil {
			return err
		}
		w.lastHost = util.SiteKey(c.Request.Host)
		if w.Strategy == "adaptive" && w.step%5 == 4 {
			w.metrics.IncrCounter([]string{"frontier", "exploration"}, 1)
		}
		if c.Discovered > 0 {
			w.metrics.AddSample([]string{"frontier", "scheduling_latency_ms"}, float32(time.Since(time.Unix(0, c.Discovered)).Milliseconds()))
		}
		w.metrics.IncrCounter([]string{"frontier", "published"}, 1)
		w.step++
		if freshRepeatedTurn {
			w.repeatedStep++
		}
		published++
		reason = "requests published"
		remaining--
	}
	return nil
}

func (w *Worker) SetMetricSink(sink metrics.MetricSink) { w.metrics = sink }

func (w *Worker) SetFlow(controller *flow.Controller, p flow.Policy) {
	w.Flow = controller
	w.RetryShare = p.RetryShare
	w.Window = p.CandidateWindow
	w.BatchSize = p.RefillBatch
}

func retryTurn(step uint64, share float64) bool {
	return math.Floor(float64(step+1)*share) > math.Floor(float64(step)*share)
}
