package application

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/extractors"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/resource/bloom"
	"github.com/iakinsey/delver/resource/maps"
	"github.com/iakinsey/delver/resource/objectstore"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/worker"
	"github.com/iakinsey/delver/worker/accumulator"
	"github.com/iakinsey/delver/worker/extractor"
	"github.com/iakinsey/delver/worker/fetcher"
	"github.com/iakinsey/delver/worker/publisher"
	"github.com/iakinsey/delver/worker/transformer"
	"net/url"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
)

func decodeParameters(raw json.RawMessage, target interface{}) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
func validateParameters(app config.Application, resources map[string]config.Resource) error {
	if app.Config.Fetch != nil {
		if err := app.Config.Fetch.Validate(); err != nil {
			return err
		}
	}
	http := app.Config.HTTPClient
	if http.ConnectTimeout < 0 || http.TLSTimeout < 0 || http.HeaderTimeout < 0 || http.BodyTimeout < 0 {
		return fmt.Errorf("network phase timeouts must be nonnegative; zero inherits the overall timeout")
	}
	if http.RequireProxy && http.Socks5Url == "" && http.HTTPProxyUrl == "" {
		return fmt.Errorf("this crawl requires a proxy; choose and configure a proxy connection")
	}
	if http.Socks5Url != "" && http.HTTPProxyUrl != "" {
		return fmt.Errorf("choose a single proxy connection")
	}
	if http.IsolateProxySessions {
		if http.Socks5Url == "" {
			return fmt.Errorf("separate proxy sessions require a SOCKS5 connection")
		}
		_, auth, err := util.ParseSOCKSProxy(http.Socks5Url)
		if err != nil {
			return err
		}
		if auth != nil {
			return fmt.Errorf("automatic proxy session labels require a SOCKS5 address without authentication credentials")
		}
	}
	if _, err := util.NormalizeHostSuffixes(http.AllowedHostSuffixes); err != nil {
		return err
	}
	if http.RetryDelay < 0 || http.RetryMaxDelay < 0 || (http.RetryDelay > 0 && http.RetryMaxDelay > 0 && http.RetryMaxDelay < http.RetryDelay) {
		return fmt.Errorf("invalid retry backoff bounds")
	}
	if strings.ContainsAny(http.UserAgent, "\r\n") {
		return fmt.Errorf("invalid user agent")
	}

	for _, pattern := range []string{http.AllowPattern, http.DenyPattern} {
		if pattern != "" {
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("invalid HTTP URL rule: %w", err)
			}
		}
	}
	if app.Config.Robots.Expiration <= 0 || app.Config.PersistentMap.GCInterval <= 0 || app.Config.PersistentMap.GCDiscardRatio <= 0 || app.Config.PersistentMap.GCDiscardRatio >= 1 {
		return fmt.Errorf("invalid robots or persistent-map timing/bounds")
	}
	if http.Timeout <= 0 || (http.MaxRetries < 0 || http.MaxRetries > 10) || http.MaxResponseBytes < 1 || http.PerOriginConcurrency < 1 || http.RequestDelay < 0 {
		return fmt.Errorf("invalid HTTP client limits")
	}
	if http.HTTPProxyUrl != "" {
		proxy, err := url.Parse(http.HTTPProxyUrl)
		if err != nil || proxy.Hostname() == "" || (proxy.Scheme != "http" && proxy.Scheme != "https") {
			return fmt.Errorf("invalid HTTP proxy URL")
		}
	}
	if http.Socks5Url != "" {
		if _, _, err := util.ParseSOCKSProxy(http.Socks5Url); err != nil {
			return fmt.Errorf("invalid SOCKS5 proxy address")
		}
	}
	paths := map[string]string{}
	for _, r := range app.Resources {
		var target interface{}
		switch r.Type {
		case "frontier_store":
			target = &frontier.StoreParams{Retention: frontier.DefaultRetention()}
		case "crawl_queue":
			target = &queue.CrawlQueueParams{MaxAttempts: 3}
		case "file_queue":
			target = &queue.FileQueueParams{}
		case "timer":
			target = &queue.TimerQueueParams{}
		case "bloom_filter":
			target = &bloom.BloomFilterParams{}
		case "rolling_bloom_filter":
			target = &bloom.RollingBloomFilterParams{}
		case "persistent_map":
			target = &maps.PersistentMapParams{}
		case "multi_host_map":
			target = &maps.MultiHostMapParams{}
		case "filesystem_object_store":
			target = &objectstore.FilesystemObjectStoreParams{}
		}
		if target != nil {
			if err := decodeParameters(r.Parameters, target); err != nil {
				return fmt.Errorf("resource %s: %w", r.Name, err)
			}
		}
		if target != nil {
			value := reflect.ValueOf(target).Elem()
			for _, name := range []string{"Path", "BasePath", "DlqPath"} {
				field := value.FieldByName(name)
				if field.IsValid() && field.Kind() == reflect.String && field.String() != "" {
					absolute, err := filepath.Abs(field.String())
					if err != nil {
						return err
					}
					if owner, ok := paths[absolute]; ok {
						return fmt.Errorf("resources %s and %s share storage directory %s", owner, r.Name, absolute)
					}
					paths[absolute] = r.Name
				}
			}
		}
		switch p := target.(type) {
		case *frontier.StoreParams:
			if paramsErr := paramsFrontier(*p); paramsErr != nil {
				return fmt.Errorf("resource %s: %w", r.Name, paramsErr)
			}
		case *maps.PersistentMapParams:
			if p.Path == "" {
				return fmt.Errorf("resource %s requires path", r.Name)
			}
		case *maps.MultiHostMapParams:
			if p.BasePath == "" {
				return fmt.Errorf("resource %s requires base_path", r.Name)
			}
		case *objectstore.FilesystemObjectStoreParams:
			if p.Path == "" {
				return fmt.Errorf("resource %s requires path", r.Name)
			}
		case *queue.CrawlQueueParams:
			if p.Path == "" || p.CrawlID == "" || p.MaxAttempts < 1 {
				return fmt.Errorf("resource %s: invalid crawl queue parameters", r.Name)
			}
		case *queue.FileQueueParams:
			if p.Path == "" || p.DlqPath == "" || filepath.Clean(p.Path) == filepath.Clean(p.DlqPath) || !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(p.Name) || p.MaxSize < 0 || p.MaxAttempts < 0 || p.MaxPollDelayMs < 0 {
				return fmt.Errorf("resource %s: invalid queue paths or bounds", r.Name)
			}
		case *queue.TimerQueueParams:
			if p.Delay <= 0 {
				return fmt.Errorf("resource %s: delay must be positive", r.Name)
			}
		case *bloom.BloomFilterParams:
			if p.MaxN == 0 || p.P <= 0 || p.P >= 1 {
				return fmt.Errorf("resource %s: invalid Bloom parameters", r.Name)
			}
		case *bloom.RollingBloomFilterParams:
			if p.MaxN == 0 || p.P <= 0 || p.P >= 1 || p.BloomCount < 1 || p.SaveInterval < 0 {
				return fmt.Errorf("resource %s: invalid rolling Bloom parameters", r.Name)
			}
		}
	}
	for _, w := range app.Workers {
		if w.Once && (w.Manager != "job" || resources["source_progress"].Type != "persistent_map") {
			return fmt.Errorf("once sources require a job manager and source_progress map")
		}
		if err := validateFrontierWorker(w, resources); err != nil {
			return err
		}
		var target interface{}
		switch w.Type {
		case "jsonl_sink":
			target = &worker.JSONLParams{}
		case "http_fetcher":
			target = &fetcher.HttpFetcherParams{}
		case "composite_extractor":
			target = &extractor.CompositeArgs{}
		case "dfs_basic_accumulator":
			target = &accumulator.DfsBasicAccumulatorParams{}
		case "news_accumulator":
			target = &accumulator.NewsAccumulatorParams{}
		case "dfs_basic_publisher":
			target = &publisher.DfsBasicPublisherParams{}
		case "fixed_seed_publisher":
			target = &publisher.FixedSeedPublisherParams{}
		case "rss_feed_publisher":
			target = &publisher.RssFeedPublisherParams{}
		case "transformer":
			target = &transformer.TransformerParams{}
		}
		if target == nil {
			continue
		}
		var fields map[string]json.RawMessage
		if len(w.Parameters) > 0 {
			if err := json.Unmarshal(w.Parameters, &fields); err != nil {
				return fmt.Errorf("worker %s: %w", w.Name, err)
			}
		}
		clean := map[string]json.RawMessage{}
		for k, v := range fields {
			clean[k] = v
		}
		typ := reflect.TypeOf(target).Elem()
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			key := field.Tag.Get("resource")
			if key == "" {
				continue
			}
			var name string
			if raw, ok := clean[key]; ok {
				if err := json.Unmarshal(raw, &name); err != nil {
					return fmt.Errorf("worker %s resource %s: %w", w.Name, key, err)
				}
				delete(clean, key)
			} else if key == config.TransformerQueueName || key == "visited_urls" || key == "seen_urls" {
				continue
			} else {
				return fmt.Errorf("worker %s missing resource %s", w.Name, key)
			}
			resource, ok := resources[name]
			if !ok {
				return fmt.Errorf("worker %s references missing resource %s", w.Name, name)
			}
			if resource.Type == "timer" {
				return fmt.Errorf("worker %s cannot publish through timer resource %s", w.Name, name)
			}
			var resourceType reflect.Type
			switch resource.Type {
			case "file_queue", "channel_queue", "crawl_queue", "timer":
				resourceType = reflect.TypeOf((*queue.Queue)(nil)).Elem()
			case "bloom_filter", "rolling_bloom_filter":
				resourceType = reflect.TypeOf((*bloom.BloomFilter)(nil)).Elem()
			case "persistent_map", "multi_host_map":
				resourceType = reflect.TypeOf((*maps.Map)(nil)).Elem()
			case "filesystem_object_store":
				resourceType = reflect.TypeOf((*objectstore.ObjectStore)(nil)).Elem()
			}
			if resourceType != nil && !resourceType.AssignableTo(field.Type) {
				return fmt.Errorf("worker %s resource %s has incompatible type %s", w.Name, key, resource.Type)
			}
		}
		raw, _ := json.Marshal(clean)
		if err := decodeParameters(raw, target); err != nil {
			return fmt.Errorf("worker %s: %w", w.Name, err)
		}
		switch params := target.(type) {
		case *worker.JSONLParams:
			if params.Path == "" {
				return fmt.Errorf("worker %s requires output path", w.Name)
			}
		case *publisher.FixedSeedPublisherParams:
			for _, uri := range params.Uris {
				parsed, err := url.Parse(uri)
				if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
					return fmt.Errorf("worker %s invalid seed URL %s", w.Name, uri)
				}
			}
		case *publisher.RssFeedPublisherParams:
			for _, uri := range params.Uris {
				parsed, err := url.Parse(uri)
				if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
					return fmt.Errorf("worker %s invalid feed URL", w.Name)
				}
			}
		case *publisher.DfsBasicPublisherParams:
			if params.UrlStorePath == "" || params.RotateAfter < 0 {
				return fmt.Errorf("worker %s invalid DFS publisher path or rotation", w.Name)
			}
		case *accumulator.NewsAccumulatorParams:
			if params.MaxDepth < 0 {
				return fmt.Errorf("worker %s negative depth", w.Name)
			}
		case *transformer.TransformerParams:
			for _, enabled := range params.Enabled {
				if enabled != "composite" && enabled != "metric" {
					return fmt.Errorf("worker %s unknown transformer %s", w.Name, enabled)
				}
			}
		case *accumulator.DfsBasicAccumulatorParams:
			if params.MaxDepth < 0 {
				return fmt.Errorf("worker %s negative depth", w.Name)
			}
		}
		if w.Type != "transformer" && w.Type != "jsonl_sink" && w.Type != "dfs_basic_publisher" && w.Outbox == "" {
			return fmt.Errorf("worker %s requires an outbox", w.Name)
		}
		if p, ok := target.(*publisher.RssFeedPublisherParams); ok && p.MaxConcurrency < 0 {
			return fmt.Errorf("worker %s max_concurrency must be nonnegative", w.Name)
		}
		if p, ok := target.(*extractor.CompositeArgs); ok {
			if p.Timeout < 0 {
				return fmt.Errorf("worker %s timeout must be nonnegative", w.Name)
			}
			names := map[string]bool{}
			known := map[string][]string{"url": nil, "text": nil, "title": nil, "language": {"text"}, "sentiment": {"language", "title"}, "country": {"text"}, "company_name": {"text"}, "adversarial": {"url"}, "ngram": {"text"}}
			for _, name := range p.Enabled {
				if names[name] {
					return fmt.Errorf("worker %s duplicate extractor %s", w.Name, name)
				}
				if registration, ok := extractors.Registered(name); ok {
					known[name] = registration.Requires
				}
				if _, ok := known[name]; !ok {
					return fmt.Errorf("worker %s unknown extractor %s", w.Name, name)
				}
				names[name] = true
			}
			visiting := map[string]bool{}
			visited := map[string]bool{}
			var visit func(string) error
			visit = func(name string) error {
				if visiting[name] {
					return fmt.Errorf("worker %s extractor dependency cycle at %s", w.Name, name)
				}
				if visited[name] {
					return nil
				}
				visiting[name] = true
				for _, dep := range known[name] {
					if err := visit(dep); err != nil {
						return err
					}
				}
				visiting[name] = false
				visited[name] = true
				return nil
			}
			for name := range names {
				if err := visit(name); err != nil {
					return err
				}
			}
			for name := range names {
				for _, dep := range known[name] {
					if !names[dep] {
						return fmt.Errorf("worker %s extractor %s requires %s", w.Name, name, dep)
					}
				}
			}
		}
	}
	return nil
}

func validateMessageFlow(app config.Application) error {
	outputKind := map[string]string{"http_fetcher": "response", "composite_extractor": "analysis", "dfs_basic_accumulator": "request", "news_accumulator": "request", "fixed_seed_publisher": "request", "rss_feed_publisher": "request"}
	inputKind := map[string]string{"http_fetcher": "request", "composite_extractor": "response", "dfs_basic_accumulator": "analysis", "news_accumulator": "analysis"}
	for _, producer := range app.Workers {
		kind := outputKind[producer.Type]
		if kind == "" || producer.Outbox == "" {
			continue
		}
		for _, consumer := range app.Workers {
			if consumer.Inbox == producer.Outbox && inputKind[consumer.Type] != "" && inputKind[consumer.Type] != kind {
				return fmt.Errorf("queue %s connects incompatible workers %s and %s", producer.Outbox, producer.Name, consumer.Name)
			}
		}
	}
	return nil
}
