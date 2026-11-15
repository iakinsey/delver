package util

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iakinsey/delver/config"

	"golang.org/x/net/proxy"
)

const defaultUserAgent = "delver"

type DelverHTTPClient interface {
	Perform(url string) (*http.Response, error)
}

type delverHTTPClient struct {
	configError               error
	HostSuffixes              []string
	RetryDelay, RetryMaxDelay time.Duration
	NetworkTimeout            time.Duration
	LocalGate, GlobalGate     config.HTTPGate
	HTTP                      *http.Client
	UserAgent                 string
	MaxRetries                int
	Policy                    func(*url.URL) error
	PolicyContext             func(context.Context, *url.URL) error
	Delay                     time.Duration
	Concurrency               int
}

// TODO use a mocking library if this becomes a common pattern
type MockDelverHTTPClient struct {
	Response *http.Response
	Error    error
}

func NewHTTPClient() DelverHTTPClient {
	return NewHTTPClientWithConfig(config.Get().HTTPClient)
}
func NewHTTPClientWithConfig(params config.HTTPClientConfig) DelverHTTPClient {
	if params.GlobalGate != nil && params.LocalGate == nil {
		params.LocalGate = NewOriginGate()
	}
	client := &http.Client{Timeout: params.Timeout}
	if params.GlobalGate != nil {
		client.Timeout = 0
	}
	var configError error
	suffixes, err := NormalizeHostSuffixes(params.AllowedHostSuffixes)
	if err != nil {
		configError = err
	}
	if params.RequireProxy && params.Socks5Url == "" && params.HTTPProxyUrl == "" {
		configError = fmt.Errorf("a configured proxy connection is required")
	}
	if params.Socks5Url != "" && params.HTTPProxyUrl != "" {
		configError = fmt.Errorf("choose a single proxy connection")
	}
	if params.IsolateProxySessions && params.Socks5Url == "" {
		configError = fmt.Errorf("separate proxy sessions require SOCKS5")
	}
	transport := &http.Transport{IdleConnTimeout: 90 * time.Second}
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = base.Clone()
	}
	// The request deadline also bounds TLS negotiation. Do not retain the
	// default transport's shorter handshake cutoff on slower connections.
	transport.TLSHandshakeTimeout = params.Timeout
	if params.Socks5Url != "" {
		address, auth, err := ParseSOCKSProxy(params.Socks5Url)
		if err != nil {
			configError = err
		} else {
			if params.IsolateProxySessions {
				if auth != nil {
					configError = fmt.Errorf("automatic proxy session labels cannot replace authentication credentials")
				} else {
					key := params.ProxySessionKey
					if key == "" {
						var entropy [32]byte
						if _, err := rand.Read(entropy[:]); err != nil {
							configError = fmt.Errorf("proxy session initialization failed")
						} else {
							key = hex.EncodeToString(entropy[:])
						}
					}
					user := sha256.Sum256([]byte("proxy-user:" + key))
					password := sha256.Sum256([]byte("proxy-password:" + key))
					auth = &proxy.Auth{User: "session-" + hex.EncodeToString(user[:16]), Password: hex.EncodeToString(password[:])}
				}
			}
			dialer, err := proxy.SOCKS5("tcp", address, auth, proxy.Direct)
			if err != nil {
				configError = fmt.Errorf("cannot configure SOCKS5 dialer")
			} else if contextual, ok := dialer.(proxy.ContextDialer); ok {
				transport.Proxy = nil
				transport.DialContext = contextual.DialContext
			} else {
				configError = fmt.Errorf("SOCKS5 dialer requires context support")
			}
		}
	} else if params.HTTPProxyUrl != "" {
		address, err := url.Parse(params.HTTPProxyUrl)
		if err != nil || address.Hostname() == "" || (address.Scheme != "http" && address.Scheme != "https") {
			configError = fmt.Errorf("invalid HTTP proxy address")
		} else {
			transport.Proxy = http.ProxyURL(address)
		}
	}
	if strings.ContainsAny(params.UserAgent, "\r\n") {
		configError = fmt.Errorf("invalid user agent")
	}
	client.Transport = transport

	return &delverHTTPClient{
		configError: configError, HostSuffixes: suffixes,
		RetryDelay: params.RetryDelay, RetryMaxDelay: params.RetryMaxDelay,
		HTTP:           client,
		NetworkTimeout: params.Timeout,
		LocalGate:      params.LocalGate, GlobalGate: params.GlobalGate,
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
	if s.configError != nil {
		return nil, s.configError
	}
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
		delay := s.RetryDelay
		if delay <= 0 {
			delay = 100 * time.Millisecond
		}
		maximum := s.RetryMaxDelay
		if maximum <= 0 {
			maximum = 5 * time.Second
		}
		if delay > maximum {
			delay = maximum
		}
		for i := 0; i < attempt && delay < maximum; i++ {
			if delay > maximum/2 {
				delay = maximum
			} else {
				delay *= 2
			}
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
func SetHTTPPolicyContext(client DelverHTTPClient, policy func(context.Context, *url.URL) error) {
	if c, ok := client.(*delverHTTPClient); ok {
		c.PolicyContext = policy
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
	if !HostnameAllowed(req.URL, s.owner.HostSuffixes) {
		return nil, fmt.Errorf("destination hostname is outside the allowed suffixes")
	}

	if policy, ok := req.Context().Value(requestPolicyKey{}).(func(*url.URL) error); ok {
		if err := policy(req.URL); err != nil {
			return nil, err
		}
	}
	if s.owner.PolicyContext != nil {
		if err := s.owner.PolicyContext(req.Context(), req.URL); err != nil {
			return nil, err
		}
	}
	if s.owner.Policy != nil {
		if err := s.owner.Policy(req.URL); err != nil {
			return nil, err
		}
	}
	var release func()
	if s.owner.GlobalGate != nil {
		local, err := s.owner.LocalGate.Acquire(req.Context(), req.URL.Scheme+"://"+req.URL.Host, s.owner.Concurrency, s.owner.Delay)
		if err != nil {
			return nil, err
		}
		global, err := s.owner.GlobalGate.Acquire(req.Context(), req.URL.Scheme+"://"+req.URL.Host, 2, 100*time.Millisecond)
		if err != nil {
			local()
			return nil, err
		}
		release = func() { global(); local() }
	} else {
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
		release = func() { <-limit.slots }
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
	}

	if s.owner.NetworkTimeout > 0 {
		ctx, cancel := context.WithTimeout(req.Context(), s.owner.NetworkTimeout)
		req = req.Clone(ctx)
		prior := release
		release = func() { cancel(); prior() }
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

// Close releases this client's idle connections without touching other crawls.
func (s *delverHTTPClient) Close() error { s.HTTP.CloseIdleConnections(); return nil }
