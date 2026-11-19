package application

import (
	"encoding/json"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/types/errs"
	"github.com/iakinsey/delver/util"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

type Termination struct {
	WhenDrained bool   `json:"when_drained"`
	MaxRequests int64  `json:"max_requests"`
	Duration    string `json:"duration"`
}
type ProxySettings struct {
	Mode string `json:"mode"`
	URL  string `json:"url"`
}
type CrawlDefinition struct {
	ExcludedExtensions string             `json:"excluded_extensions,omitempty"`
	Fetch              *flow.Policy       `json:"fetch,omitempty"`
	Network            NetworkSettings    `json:"network"`
	Proxy              ProxySettings      `json:"proxy"`
	Name               string             `json:"name"`
	Seeds              []string           `json:"seeds"`
	Feeds              []string           `json:"feeds"`
	Scope              string             `json:"scope"`
	Strategy           string             `json:"strategy"`
	MaxDepth           int                `json:"max_depth"`
	MaxHops            int                `json:"max_hops"`
	AllowPattern       string             `json:"allow_pattern"`
	DenyPattern        string             `json:"deny_pattern"`
	RequestDelay       string             `json:"request_delay"`
	Concurrency        int                `json:"concurrency"`
	Revisit            string             `json:"revisit"`
	Retention          frontier.Retention `json:"retention"`
	Termination        Termination        `json:"termination"`
}

func DefaultCrawlDefinition() CrawlDefinition {
	p := flow.Default()
	return CrawlDefinition{Fetch: &p, Network: DefaultNetworkSettings(), Name: "Untitled crawl", Seeds: []string{}, Feeds: []string{}, Scope: "same_domain", Strategy: "host_diverse", MaxDepth: 10, MaxHops: 3, RequestDelay: "100ms", Concurrency: 2, Revisit: "24h", Retention: frontier.DefaultRetention()}
}
func decodeDefinition(raw json.RawMessage) (CrawlDefinition, error) {
	d := DefaultCrawlDefinition()
	d.Fetch = nil // Omitted settings preserve existing four-worker definitions.
	if err := decodeParameters(raw, &d); err != nil {
		return d, errs.NewRequestError(err.Error())
	}
	extensions, err := util.NormalizeExtensions(d.ExcludedExtensions)
	if err != nil {
		return d, errs.NewRequestError(err.Error())
	}
	d.ExcludedExtensions = strings.Join(extensions, ", ")
	return d, nil
}
func CompileCrawl(root, runID string, d CrawlDefinition) (config.Application, error) {
	if strings.TrimSpace(d.Name) == "" || len(d.Seeds)+len(d.Feeds) == 0 {
		return config.Application{}, errs.NewRequestError("name and at least one seed URL or feed required")
	}
	for _, raw := range append(append([]string{}, d.Seeds...), d.Feeds...) {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return config.Application{}, errs.NewRequestError("seed URLs must use HTTP or HTTPS without credentials")
		}
	}
	if d.MaxDepth < 1 || d.MaxHops < 1 || d.Concurrency < 1 || d.Concurrency > 8192 {
		return config.Application{}, errs.NewRequestError("depth and hop limits must be positive; per-host concurrency must be between 1 and 8192")
	}
	delay, err := time.ParseDuration(d.RequestDelay)
	if err != nil || delay < 0 {
		return config.Application{}, errs.NewRequestError("request delay must be a nonnegative duration")
	}
	revisit := 24 * time.Hour
	if !d.Termination.WhenDrained {
		revisit, err = time.ParseDuration(d.Revisit)
		if err != nil || revisit <= 0 {
			return config.Application{}, errs.NewRequestError("revisit interval must be positive")
		}
	}
	if d.Termination.MaxRequests < 0 {
		return config.Application{}, errs.NewRequestError("request budget cannot be negative")
	}
	if d.Termination.Duration != "" {
		duration, err := time.ParseDuration(d.Termination.Duration)
		if err != nil || duration <= 0 {
			return config.Application{}, errs.NewRequestError("run duration must be positive")
		}
	}
	a, err := DefaultApplication(Options{DataDirectory: root})
	if err != nil {
		return a, err
	}
	c := &a.Config
	policy := flow.Legacy()
	if d.Fetch != nil {
		policy = *d.Fetch
	}
	if err := policy.Validate(); err != nil {
		return a, err
	}
	c.Fetch = &policy
	c.API.Enabled = false
	c.Streamer.Enabled = false
	c.Metrics.Enabled = false
	switch d.Proxy.Mode {
	case "", "direct":
	case "http":
		if d.Proxy.URL == "" {
			return a, errs.NewRequestError("HTTP proxy URL required")
		}
		c.HTTPClient.HTTPProxyUrl = d.Proxy.URL
	case "socks5":
		if d.Proxy.URL == "" {
			return a, errs.NewRequestError("SOCKS5 proxy URL required")
		}
		c.HTTPClient.Socks5Url = d.Proxy.URL
	default:
		return a, errs.NewRequestError("invalid proxy mode: expected direct, http, or socks5")
	}
	if err := applyNetworkSettings(&c.HTTPClient, d.Network); err != nil {
		return a, errs.NewRequestError(err.Error())
	}
	for _, uri := range append(append([]string{}, d.Seeds...), d.Feeds...) {
		if !util.URLHostnameAllowed(uri, c.HTTPClient.AllowedHostSuffixes) {
			return a, errs.NewRequestError("seed and feed URLs must match allowed hostname suffixes")
		}
	}
	extensions, err := util.NormalizeExtensions(d.ExcludedExtensions)
	if err != nil {
		return a, errs.NewRequestError(err.Error())
	}
	c.HTTPClient.ExcludedExtensions = extensions
	c.HTTPClient.Scope = d.Scope
	c.HTTPClient.AllowPattern = d.AllowPattern
	c.HTTPClient.DenyPattern = d.DenyPattern
	c.HTTPClient.RequestDelay = delay
	c.HTTPClient.PerOriginConcurrency = d.Concurrency
	resource := func(name, kind string, p interface{}) {
		b, _ := json.Marshal(p)
		a.Resources = append(a.Resources, config.Resource{Name: name, Type: kind, Parameters: b})
	}
	worker := func(name, kind, in, out string, p interface{}, job bool, interval time.Duration, once bool) {
		b, _ := json.Marshal(p)
		count := 4
		if kind == "http_fetcher" {
			count = policy.PoolSize()
		}
		if job || kind == "jsonl_sink" {
			count = 1
		}
		manager := "worker"
		if job {
			manager = "job"
		}
		a.Workers = append(a.Workers, config.Worker{Name: name, Type: kind, Inbox: in, Outbox: out, Parameters: b, Manager: manager, Interval: interval, Once: once, Count: count})
	}
	resource("requests", "crawl_queue", map[string]interface{}{"path": filepath.Join(root, "requests"), "crawl_id": runID, "max_attempts": 3, "notify_dispatch": true, "accept_frontier_generations": true})
	for _, name := range []string{"seeds", "fetched", "analyzed", "results"} {
		resource(name, "file_queue", map[string]interface{}{"name": name, "path": filepath.Join(root, name), "dlq_path": filepath.Join(root, name+"-failed"), "max_attempts": 3, "max_poll_delay_ms": 20, "reset": true})
	}
	resource("objects", "filesystem_object_store", map[string]string{"path": filepath.Join(root, "objects")})
	resource("source_progress", "persistent_map", map[string]string{"path": filepath.Join(root, "source_progress")})
	fp := frontier.StoreParams{OriginDelay: frontier.Lifetime(delay), Retry: &frontier.RetryPolicy{Share: policy.RetryShare, Limit: c.HTTPClient.MaxRetries, Base: flow.Duration(c.HTTPClient.RetryDelay), Maximum: flow.Duration(c.HTTPClient.RetryMaxDelay), FailureThreshold: policy.FailureThreshold, HostCooldown: policy.HostCooldown, HostCooldownMax: policy.HostCooldownMax, ProbeInterval: policy.RecoveryProbeInterval}, Path: filepath.Join(root, "frontier"), Retention: d.Retention, MaxDispatches: d.Termination.MaxRequests, CacheBudgetMB: 64}
	if !d.Termination.WhenDrained {
		fp.RevisitAfter = frontier.Lifetime(revisit)
	}
	resource("frontier", "frontier_store", fp)
	if len(d.Seeds) > 0 {
		worker("starting_urls", "fixed_seed_publisher", "", "seeds", map[string]interface{}{"uris": d.Seeds}, true, time.Second, true)
	}
	if len(d.Feeds) > 0 {
		feedInterval := revisit
		if d.Termination.WhenDrained {
			feedInterval = time.Second
		}
		worker("starting_feeds", "rss_feed_publisher", "", "seeds", map[string]interface{}{"uris": d.Feeds}, true, feedInterval, d.Termination.WhenDrained)
	}
	worker("admit_seeds", "frontier_admission", "seeds", "", map[string]interface{}{"frontier_store": "frontier", "allowed_host_suffixes": c.HTTPClient.AllowedHostSuffixes}, false, 0, false)
	worker("fetch", "http_fetcher", "requests", "fetched", map[string]string{"object_store": "objects"}, false, 0, false)
	worker("extract", "composite_extractor", "fetched", "analyzed", map[string]interface{}{"object_store": "objects", "transformer_queue": "results", "enabled": []string{"url", "text", "title"}}, false, 0, false)
	worker("discover", "frontier_accumulator", "analyzed", "", map[string]interface{}{"frontier_store": "frontier", "scope": d.Scope, "allowed_host_suffixes": c.HTTPClient.AllowedHostSuffixes, "max_depth": d.MaxDepth, "max_hops": d.MaxHops, "allow_pattern": d.AllowPattern, "deny_pattern": d.DenyPattern}, false, 0, false)
	worker("save", "jsonl_sink", "results", "", map[string]string{"path": filepath.Join(root, "results.jsonl")}, false, 0, false)
	worker("publish", "frontier_publisher", "", "", map[string]interface{}{"frontier_store": "frontier", "output_queue": "requests", "strategy": d.Strategy}, true, 100*time.Millisecond, false)
	worker("cleanup", "frontier_cleanup", "", "", map[string]string{"frontier_store": "frontier"}, true, time.Duration(d.Retention.CleanupInterval), false)
	a, err = prepareApplication(a)
	if err != nil {
		return a, errs.NewRequestError(err.Error())
	}
	return a, nil
}
