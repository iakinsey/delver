package util

import (
	"log"
	"net/http"
	"time"

	"golang.org/x/net/proxy"
)

const defaultUserAgent = "delver"

type HTTPClientParams struct {
	Timeout   time.Duration
	UserAgent string
	Socks5Url string
}

type DelverHTTPClient struct {
	HTTP      *http.Client
	UserAgent string
}

func (s *DelverHTTPClient) Perform(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)

	if err != nil {
		return nil, err
	}

	if s.UserAgent != "" {
		req.Header.Set("User-Agent", s.UserAgent)
	} else {
		req.Header.Set("User-Agent", defaultUserAgent)
	}

	return s.HTTP.Do(req)
}

func NewHTTPClient(params HTTPClientParams) *DelverHTTPClient {
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
	}

	return &DelverHTTPClient{
		HTTP:      client,
		UserAgent: params.UserAgent,
	}
}
