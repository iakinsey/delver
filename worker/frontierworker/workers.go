// Package frontierworker implements frontier orchestration using ordinary workers.
package frontierworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/armon/go-metrics"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/instrument"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"golang.org/x/net/publicsuffix"
	"net"
	"net/url"
	"regexp"

	"time"
)

type Params struct {
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
	metrics metrics.MetricSink
	Params
	Kind        string
	lastHost    string
	step        uint64
	Selector    frontier.Selector
	allow, deny *regexp.Regexp
	nextCleanup time.Time
	Filters     []frontier.Filter
}

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
	if err := w.Store.Outcome(c.FetcherResponse); err != nil {
		return nil, err
	}
	var discoveries []message.FetcherRequest
	for _, link := range links {
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
	id := fmt.Sprintf("%s:%s:%d:%d:%s", c.RequestID, c.URI, c.Depth, c.Hops, m.ID)
	if err := w.Store.StageDiscovery(ctx, id, discoveries); err != nil {
		return nil, err
	}
	_, err = w.Store.DrainDiscovery(ctx, w.Window)
	return nil, err
}
func domain(h string) string {
	if net.ParseIP(h) != nil {
		return h
	}
	d, err := publicsuffix.EffectiveTLDPlusOne(h)
	if err != nil {
		return h
	}
	return d
}
func (w *Worker) publish(ctx context.Context) error {
	if target, ok := w.Store.(interface {
		DrainRecrawls(context.Context, int) error
	}); ok {
		if err := target.DrainRecrawls(ctx, w.Window); err != nil {
			return err
		}
	}
	if _, err := w.Store.DrainDiscovery(ctx, w.Window); err != nil {
		return err
	}
	n := w.Output.Len()
	if n < 0 {
		return fmt.Errorf("fetch queue unavailable")
	}
	remaining := w.LowWater - int(n)
	if remaining <= 0 {
		return nil
	}
	if remaining > w.BatchSize {
		remaining = w.BatchSize
	}
	candidates, err := w.Store.Candidates(ctx, w.Strategy, w.lastHost, w.Window)
	if err != nil {
		return err
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
		at := selector.Select(candidates, w.lastHost, w.step, scores)
		if at < 0 || at >= len(candidates) {
			return fmt.Errorf("selector returned invalid candidate index %d", at)
		}

		c := candidates[at]
		candidates = append(candidates[:at], candidates[at+1:]...)
		c, err = w.Store.Reserve(ctx, c)
		if errors.Is(err, frontier.ErrDispatchBudget) {
			continue
		}
		if errors.Is(err, frontier.ErrCooling) {
			continue
		}
		if err != nil {
			return err
		}
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
		w.lastHost = c.Request.Host
		if w.Strategy == "adaptive" && w.step%5 == 4 {
			w.metrics.IncrCounter([]string{"frontier", "exploration"}, 1)
		}
		if c.Discovered > 0 {
			w.metrics.AddSample([]string{"frontier", "scheduling_latency_ms"}, float32(time.Since(time.Unix(0, c.Discovered)).Milliseconds()))
		}
		w.metrics.IncrCounter([]string{"frontier", "published"}, 1)
		w.step++
		remaining--
	}
	return nil
}

func (w *Worker) SetMetricSink(sink metrics.MetricSink) { w.metrics = sink }
