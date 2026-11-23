package fetcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/protection"
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
	errorspkg "github.com/pkg/errors"
)

type HttpFetcherParams struct {
	ObjectStore objectstore.ObjectStore `json:"-" resource:"object_store"`
}

type httpFetcher struct {
	createMappedBody func(int64) (*mappedBody, error)
	protection       *protection.Policy
	crawlID, runID   string
	purgeDepth       int
	flow             *flow.Controller
	scope            string
	allow, deny      *regexp.Regexp
	maxBytes         int64
	robots           frontier.ClosableFilter
	ObjectStore      objectstore.ObjectStore
	Client           util.DelverHTTPClient
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
		protection: conf.Protection, crawlID: c.RuntimeCrawlID, runID: c.RuntimeRunID, purgeDepth: conf.RestrictedPurgeDepth,
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
		return nil, errorspkg.Wrap(err, "error parsing fetcher request")
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
		if errorspkg.Is(err, protection.ErrBlocked) {
			response.Error = protection.ErrBlocked.Error()
		}
		if errorspkg.Is(err, protection.ErrUnavailable) {
			response.Error = protection.ErrUnavailable.Error()
		}
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
		return key, errorspkg.New("empty HTTP response")
	}
	if res.Request != nil && res.Request.URL != nil {
		response.EffectiveURI = res.Request.URL.String()
	}
	response.HTTPCode = res.StatusCode
	response.Header = res.Header
	key = types.NewV4()

	if s.protection == nil {
		log.Printf("GET %d %s", response.HTTPCode, request.URI)
	}

	limit := s.maxBytes
	if limit <= 0 {
		limit = 16 << 20
	}
	storageStart := time.Now()
	var hash string
	var source io.Reader = &boundedBody{reader: res.Body, remaining: limit, count: &response.BytesReceived}
	if s.protection != nil {
		encoding := res.Header.Get("Content-Encoding")
		// Small bodies stay buffered: mapping and pipe setup would outweigh the
		// saved copies. Unknown/decompressed lengths retain the streaming path.
		if !res.Uncompressed && res.ContentLength >= 64<<10 && res.ContentLength <= limit && len(res.TransferEncoding) == 0 && (encoding == "" || strings.EqualFold(encoding, "identity")) {
			body, readErr := s.readMappedProtected(ctx, request.URI, response.EffectiveURI, source, res.ContentLength, res.Header.Get("Content-Type"))
			if readErr == nil {
				defer body.Close()
				if s.flow != nil {
					s.flow.Counter("body_mapped", 1)
				}
				err = s.protection.GuardMany([]string{request.URI, response.EffectiveURI}, func() error {
					if store, ok := s.ObjectStore.(objectstore.FileObjectStore); ok {
						var spliced int64
						hash, spliced, err = store.PutFile(ctx, key, body.file)
						if s.flow != nil {
							s.flow.Counter("object_splice_bytes", spliced)
							if err == nil {
								s.flow.Counter("object_buffered_bytes", response.BytesReceived-spliced)
							}
						}
						return err
					}
					hash, err = s.ObjectStore.Put(key, io.NewSectionReader(body.file, 0, res.ContentLength))
					if err == nil && s.flow != nil {
						s.flow.Counter("object_buffered_bytes", response.BytesReceived)
					}
					return err
				})
				return s.finishStorage(response, key, hash, storageStart, err)
			}
			if !errorspkg.Is(readErr, errMappedBodyUnavailable) {
				return key, readErr
			}
		}
		releaseMemory, e := s.reserveInspection(ctx, limit*2+(64<<10))
		if e != nil {
			return key, e
		}
		defer releaseMemory()
		if s.flow != nil {
			s.flow.Counter("body_buffered", 1)
		}
		raw, readErr := s.readProtected(ctx, request.URI, response.EffectiveURI, source, res.Header.Get("Content-Type"))
		if readErr != nil {
			return key, readErr
		}
		defer clear(raw)
		err = s.protection.GuardMany([]string{request.URI, response.EffectiveURI}, func() error {
			var e error
			hash, e = s.ObjectStore.Put(key, bytes.NewReader(raw))
			return e
		})
	} else {
		hash, err = s.ObjectStore.Put(key, source)
	}
	if err == nil && s.flow != nil {
		s.flow.Counter("object_buffered_bytes", response.BytesReceived)
	}
	return s.finishStorage(response, key, hash, storageStart, err)
}

func (s *httpFetcher) reserveInspection(ctx context.Context, bytes int64) (func(), error) {
	started := time.Now()
	release, err := s.protection.Reserve(ctx, bytes)
	if s.flow != nil {
		s.flow.Measure("inspection_wait", time.Since(started))
	}
	return release, err
}

func (s *httpFetcher) finishStorage(response *message.FetcherResponse, key types.UUID, hash string, storageStart time.Time, err error) (types.UUID, error) {
	if s.flow != nil {
		s.flow.Measure("storage", time.Since(storageStart))
	}
	if err == nil {
		response.BytesStored = response.BytesReceived
	}

	if err == nil {
		response.ContentMD5 = hash
	}

	return key, errorspkg.Wrap(err, "store object failure")
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
		if strings.Contains(r.Error, protection.ErrUnavailable.Error()) || strings.Contains(r.Error, "protection inspection memory budget") {
			r.FailureClass = "storage"
			return
		}
		if strings.Contains(r.Error, protection.ErrBlocked.Error()) {
			r.FailureClass = "excluded"
			r.Retryable = false
			return
		}
		text := strings.ToLower(r.Error)
		switch {
		case strings.Contains(text, "file extension excluded"):
			r.FailureClass = "excluded"
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
			if strings.Contains(text, "eof") || strings.Contains(text, "read tcp") || strings.Contains(text, "gzip:") || strings.Contains(text, "flate:") {
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

func (s *httpFetcher) readProtected(ctx context.Context, original, effective string, r io.Reader, contentType string) ([]byte, error) {
	if effective == "" {
		effective = original
	}
	if e := s.protection.Check(effective); e != nil {
		return nil, e
	}
	if textualResponse(contentType) {
		inspector := s.protection.InspectReader(ctx, r, effective, s.crawlID, s.runID, s.purgeDepth)
		defer inspector.Close()
		r = inspector
	}
	var out bytes.Buffer
	buffer := make([]byte, 32<<10)
	defer clear(buffer)
	for {
		if e := ctx.Err(); e != nil {
			clear(out.Bytes())
			return nil, e
		}
		n, e := r.Read(buffer)
		if n > 0 {
			out.Write(buffer[:n])
		}
		if e == io.EOF {
			return out.Bytes(), nil
		}
		if e != nil {
			clear(out.Bytes())
			return nil, e
		}
	}
}

func (s *httpFetcher) PurgeMatching(ctx context.Context, match func(string) bool) error {
	for _, resource := range []interface{}{s.robots, s.Client} {
		if p, ok := resource.(interface {
			PurgeMatching(context.Context, func(string) bool) error
		}); ok {
			if e := p.PurgeMatching(ctx, match); e != nil {
				return e
			}
		}
	}
	return nil
}
