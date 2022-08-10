package util

import (
	"net/http"
	"net/url"

	"github.com/iakinsey/delver/config"
	log "github.com/sirupsen/logrus"

	"golang.org/x/net/proxy"
)

const defaultUserAgent = "delver"

type DelverHTTPClient interface {
	Perform(url string) (*http.Response, error)
}

type delverHTTPClient struct {
	HTTP       *http.Client
	UserAgent  string
	MaxRetries int
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
			log.Fatalf("failed to create http client dialer %s", err)
		}

		if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
			client.Transport = &http.Transport{
				DialContext: contextDialer.DialContext,
			}
		} else {
			log.Fatalf("unable to generate context dialer")
		}
	} else if params.HTTPProxyUrl != "" {
		if url, err := url.Parse(params.HTTPProxyUrl); err == nil {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(url),
			}
		} else {
			log.Fatalf("failed to parse http proxy string %s %s", params.HTTPProxyUrl, err)
		}
	}

	return &delverHTTPClient{
		HTTP:       client,
		UserAgent:  params.UserAgent,
		MaxRetries: params.MaxRetries,
	}
}

func (s *delverHTTPClient) Perform(url string) (*http.Response, error) {
	var req *http.Request
	var resp *http.Response
	var err error

	for i := 0; i < s.MaxRetries+1; i++ {
		req, err = http.NewRequest("GET", url, nil)

		if err != nil {
			return nil, err
		}

		if s.UserAgent != "" {
			req.Header.Set("User-Agent", s.UserAgent)
		} else {
			req.Header.Set("User-Agent", defaultUserAgent)
		}

		resp, err = s.HTTP.Do(req)

		if err == nil {
			break
		}
	}

	return resp, err
}

func (s *MockDelverHTTPClient) Perform(url string) (*http.Response, error) {
	return s.Response, s.Error
}
