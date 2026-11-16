package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/frontier"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/resource/objectstore"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/worker"
	"github.com/pkg/errors"
)

type HttpFetcherParams struct {
	ObjectStore objectstore.ObjectStore `json:"-" resource:"object_store"`
}

type httpFetcher struct {
	flow        *flow.Controller
	scope       string
	allow, deny *regexp.Regexp
	maxBytes    int64
	robots      frontier.ClosableFilter
	ObjectStore objectstore.ObjectStore
	Client      util.DelverHTTPClient
}

func NewHttpFetcher(args HttpFetcherParams) worker.Worker {
	return NewHttpFetcherWithConfig(args, config.Get())
}
func NewHttpFetcherWithConfig(args HttpFetcherParams, c config.Config) worker.Worker {
	client := util.NewHTTPClientWithConfig(c.HTTPClient)
	conf := c.HTTPClient
	robots := frontier.NewMemoryRobotsWithConfig(c)
	util.SetHTTPPolicyContext(client, func(ctx context.Context, u *url.URL) error {
		if len(conf.AllowedDomains) > 0 {
			allowed := false
			for _, domain := range conf.AllowedDomains {
				if strings.EqualFold(u.Hostname(), domain) || strings.EqualFold(util.GetSLDAndTLD(strings.ToLower(u.Hostname())), domain) {
					allowed = true
				}
			}
			if !allowed {
				return fmt.Errorf("URL outside allowed domains: %s", u)
			}
		}
		if conf.RespectRobots {
			allowed, err := robots.IsAllowedContext(ctx, u.String())
			if err != nil {
				return err
			}
			if !allowed {
				return fmt.Errorf("robots disallows %s", u)
			}
		}
		return nil
	})
	var allow, deny *regexp.Regexp
	if conf.AllowPattern != "" {
		allow = regexp.MustCompile(conf.AllowPattern)
	}
	if conf.DenyPattern != "" {
		deny = regexp.MustCompile(conf.DenyPattern)
	}
	return &httpFetcher{
		flow:  c.HTTPClient.Flow,
		allow: allow, deny: deny,
		ObjectStore: args.ObjectStore,
		maxBytes:    c.HTTPClient.MaxResponseBytes,
		scope:       c.HTTPClient.Scope,
		robots:      robots,
		Client:      client,
	}
}

func (s *httpFetcher) OnMessage(msg types.Message) (interface{}, error) {
	return s.OnMessageContext(context.Background(), msg)
}
func (s *httpFetcher) OnMessageContext(ctx context.Context, msg types.Message) (interface{}, error) {
	request := message.FetcherRequest{}

	if err := json.Unmarshal(msg.Message, &request); err != nil {
		return nil, errors.Wrap(err, "error parsing fetcher request")
	}

	if request.ScopeRoot == "" && (s.scope != "" || s.allow != nil || s.deny != nil) {
		request.ScopeRoot = request.URI
	}
	if request.ScopeRoot != "" {
		ctx = util.WithHTTPPolicy(ctx, func(u *url.URL) error {
			if s.allow != nil && !s.allow.MatchString(u.String()) || s.deny != nil && s.deny.MatchString(u.String()) {
				return fmt.Errorf("URL excluded by crawl rules")
			}
			if !util.ScopeAllows(s.scope, request.ScopeRoot, u.String()) {
				return fmt.Errorf("redirect outside crawl boundaries")
			}
			return nil
		})
	}
	response := message.FetcherResponse{
		FetcherRequest: request,
	}

	if s.flow != nil && request.QueuedAt > 0 {
		s.flow.Measure("queue_wait", time.Since(time.Unix(0, request.QueuedAt)))
	}
	s.doHttpRequestWithRetry(flow.FetchAttempt(ctx), request, &response)
	classifyResponse(&response)
	if s.flow != nil {
		s.flow.Record(response.HTTPCode, response.Success && response.HTTPCode >= 200 && response.HTTPCode < 400, strings.Contains(strings.ToLower(http.Header(response.Header).Get("Content-Type")), "text/html"), response.FailureClass, time.Duration(response.ElapsedTimeMs)*time.Millisecond, response.BytesReceived, response.BytesStored)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return response, nil
}

func (s *httpFetcher) OnComplete() {
	if s.robots != nil {
		s.robots.Close()
	}
	if client, ok := s.Client.(interface{ Close() error }); ok {
		client.Close()
	}
}

func (s *httpFetcher) doHttpRequestWithRetry(ctx context.Context, request message.FetcherRequest, response *message.FetcherResponse) {
	var key types.UUID
	var err error

	start := time.Now()
	response.Timestamp = start.Unix()

	key, err = s.doHttpRequest(ctx, request, response)

	if err == nil {
		response.StoreKey = key
	}

	response.Success = err == nil
	response.ElapsedTimeMs = time.Since(start).Milliseconds()

	if err != nil {
		response.Error = err.Error()
	}
}

func (s *httpFetcher) doHttpRequest(ctx context.Context, request message.FetcherRequest, response *message.FetcherResponse) (key types.UUID, err error) {
	var res *http.Response
	if client, ok := s.Client.(interface {
		PerformContext(context.Context, string) (*http.Response, error)
	}); ok {
		res, err = client.PerformContext(ctx, request.URI)
	} else {
		res, err = s.Client.Perform(request.URI)
	}

	if err != nil {
		return key, err
	}

	if res != nil {
		defer res.Body.Close()
	}

	if res == nil || res.Body == nil {
		return key, errors.New("empty HTTP response")
	}
	if res.Request != nil && res.Request.URL != nil {
		response.EffectiveURI = res.Request.URL.String()
	}
	response.HTTPCode = res.StatusCode
	response.Header = res.Header
	key = types.NewV4()

	log.Printf("GET %d %s", response.HTTPCode, request.URI)

	limit := s.maxBytes
	if limit <= 0 {
		limit = 16 << 20
	}
	storageStart := time.Now()
	hash, err := s.ObjectStore.Put(key, &boundedBody{reader: res.Body, remaining: limit, count: &response.BytesReceived})
	if s.flow != nil {
		s.flow.Measure("storage", time.Since(storageStart))
	}
	if err == nil {
		response.BytesStored = response.BytesReceived
	}

	if err == nil {
		response.ContentMD5 = hash
	}

	return key, errors.Wrap(err, "store object failure")
}

type boundedBody struct {
	count     *int64
	reader    io.Reader
	remaining int64
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		var probe [1]byte
		n, err := b.reader.Read(probe[:])
		if n > 0 {
			return 0, fmt.Errorf("response exceeds configured size limit")
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.reader.Read(p)
	b.remaining -= int64(n)
	if b.count != nil {
		*b.count += int64(n)
	}
	return n, err
}

func classifyResponse(r *message.FetcherResponse) {
	if r.Error != "" {
		text := strings.ToLower(r.Error)
		switch {
		case strings.Contains(text, "outside"), strings.Contains(text, "excluded"), strings.Contains(text, "robots disallows"), strings.Contains(text, "invalid http"), strings.Contains(text, "configured size limit"):
			r.FailureClass = "policy"
		case strings.Contains(text, "context canceled"):
			r.FailureClass = "cancelled"
		case strings.Contains(text, "proxyconnect"), strings.Contains(text, "proxy connection"):
			r.FailureClass = "proxy"
			r.Retryable = true
		case strings.Contains(text, "deadline"), strings.Contains(text, "timeout"):
			r.FailureClass = "timeout"
			r.Retryable = true
		case strings.Contains(text, "store object failure"):
			r.FailureClass = "storage"
			if strings.Contains(text, "eof") || strings.Contains(text, "read tcp") {
				r.FailureClass = "body"
				r.Retryable = true
			}
		default:
			r.FailureClass = "connection"
			r.Retryable = true
		}
	} else if r.HTTPCode == 429 {
		r.FailureClass = "http_429"
		r.Retryable = true
	} else if r.HTTPCode >= 500 {
		r.FailureClass = "http_5xx"
		r.Retryable = true
	}
	if r.FailureClass == "" && r.HTTPCode >= 400 {
		r.FailureClass = "http_4xx"
	}
	value := http.Header(r.Header).Get("Retry-After")
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds >= 0 {
		r.RetryAfterMS = seconds * 1000
	} else if when, err := http.ParseTime(value); err == nil && when.After(time.Now()) {
		r.RetryAfterMS = time.Until(when).Milliseconds()
	}
}
