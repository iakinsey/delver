package util

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/iakinsey/delver/config"
	log "github.com/sirupsen/logrus"

	"golang.org/x/net/proxy"
)

const defaultUserAgent = "delver"

type DelverHTTPClient interface {
	Perform(url string) (*http.Response, error)
}

type delverHTTPClient struct {
	HTTP        *http.Client
	UserAgent   string
	MaxRetries  int
	Policy      func(*url.URL) error
	Delay       time.Duration
	Concurrency int
}

// TODO use a mocking library if this becomes a common pattern
type MockDelverHTTPClient struct {
	Response *http.Response
	Error    error
}

func NewHTTPClient() DelverHTTPClient {
	params := config.Get().HTTPClient
	client := &http.Client{Timeout: params.Timeout}

	if params.Socks5Url != "" {
		dialer, err := proxy.SOCKS5("tcp", params.Socks5Url, nil, proxy.Direct)

		if err != nil {
			log.Panicf("failed to create http client dialer %s", err)
		}

		if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
			client.Transport = &http.Transport{
				DialContext: contextDialer.DialContext,
			}
		} else {
			log.Panicf("unable to generate context dialer")
		}
	} else if params.HTTPProxyUrl != "" {
		if url, err := url.Parse(params.HTTPProxyUrl); err == nil {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(url),
			}
		} else {
			log.Panicf("failed to parse http proxy string %s %s", params.HTTPProxyUrl, err)
		}
	}

	return &delverHTTPClient{
		HTTP:        client,
		UserAgent:   params.UserAgent,
		MaxRetries:  params.MaxRetries,
		Delay:       params.RequestDelay,
		Concurrency: params.PerOriginConcurrency,
	}
}

type originLimit struct {
	slots chan struct{}
	mu    sync.Mutex
	next  time.Time
}

var originLimits sync.Map

func (s *delverHTTPClient) Perform(uri string) (*http.Response, error) {
	return s.PerformContext(context.Background(), uri)
}
func (s *delverHTTPClient) PerformContext(ctx context.Context, uri string) (*http.Response, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	if parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid HTTP URL %q", uri)
	}
	client := *s.HTTP
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = &limitedTransport{base: transport, owner: s}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}
	for attempt := 0; attempt <= s.MaxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
		if err != nil {
			return nil, err
		}
		agent := s.UserAgent
		if agent == "" {
			agent = defaultUserAgent
		}
		req.Header.Set("User-Agent", agent)
		resp, err := client.Do(req)
		retry := err != nil || (resp != nil && (resp.StatusCode == 429 || resp.StatusCode >= 500))
		if !retry || attempt == s.MaxRetries {
			return resp, err
		}
		delay := time.Duration(1<<uint(attempt)) * 100 * time.Millisecond
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}
		if resp != nil {
			if value := resp.Header.Get("Retry-After"); value != "" {
				if seconds, e := strconv.Atoi(value); e == nil && seconds >= 0 {
					delay = time.Duration(seconds) * time.Second
				} else if when, e := http.ParseTime(value); e == nil {
					delay = time.Until(when)
				}
			}
			io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
		}
		if err := waitHTTP(ctx, delay); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("invalid retry count")
}
func waitHTTP(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func SetHTTPPolicy(client DelverHTTPClient, policy func(*url.URL) error) {
	if c, ok := client.(*delverHTTPClient); ok {
		c.Policy = policy
	}
}

func (s *MockDelverHTTPClient) Perform(url string) (*http.Response, error) {
	return s.Response, s.Error
}

type limitedTransport struct {
	base  http.RoundTripper
	owner *delverHTTPClient
}

func (s *limitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if s.owner.Policy != nil {
		if err := s.owner.Policy(req.URL); err != nil {
			return nil, err
		}
	}
	count := s.owner.Concurrency
	if count < 1 {
		count = 1
	}
	value, _ := originLimits.LoadOrStore(req.URL.Scheme+"://"+req.URL.Host, &originLimit{slots: make(chan struct{}, count)})
	limit := value.(*originLimit)
	select {
	case limit.slots <- struct{}{}:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	release := func() { <-limit.slots }
	limit.mu.Lock()
	wait := time.Until(limit.next)
	if wait < 0 {
		wait = 0
	}
	limit.next = time.Now().Add(wait + s.owner.Delay)
	limit.mu.Unlock()
	if err := waitHTTP(req.Context(), wait); err != nil {
		release()
		return nil, err
	}
	response, err := s.base.RoundTrip(req)
	if err != nil {
		release()
		return response, err
	}
	if response.Body == nil {
		release()
	} else {
		response.Body = &limitedBody{ReadCloser: response.Body, release: release}
	}
	return response, nil
}

type limitedBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *limitedBody) Read(data []byte) (int, error) {
	n, err := b.ReadCloser.Read(data)
	if err != nil {
		b.once.Do(b.release)
	}
	return n, err
}
func (b *limitedBody) Close() error { err := b.ReadCloser.Close(); b.once.Do(b.release); return err }
