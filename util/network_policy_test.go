package util

import (
	"context"
	"fmt"
	"github.com/iakinsey/delver/config"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestHostnameSuffixLabelBoundariesWithoutResolution(t *testing.T) {
	suffixes, err := NormalizeHostSuffixes([]string{" .ONION ", "onion", "INTERNAL.Example."})
	if err != nil {
		t.Fatal(err)
	}
	for raw, want := range map[string]bool{"http://alpha.onion/": true, "https://sub.alpha.onion:443/": true, "http://alpha.onion./": true, "http://onion.attacker.example/": false, "http://alphaonion/": false, "http://127.0.0.1/": false, "http://sub.internal.example/": true, "ftp://alpha.onion/": false} {
		u, _ := url.Parse(raw)
		if HostnameAllowed(u, suffixes) != want {
			t.Errorf("%s: expected %v", raw, want)
		}
	}
	for _, value := range []string{"", ".", "https://example.test", "a..test", "a_test", "example:80", "example/path", "-bad.test"} {
		if _, err := NormalizeHostSuffixes([]string{value}); err == nil {
			t.Errorf("accepted invalid suffix %q", value)
		}
	}
}
func TestRequiredProxyAndProxyFailuresNeverFallBack(t *testing.T) {
	var direct atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { direct.Add(1); fmt.Fprint(w, "direct leak") }))
	defer origin.Close()
	unavailable, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := unavailable.Addr().String()
	unavailable.Close()
	for _, connection := range []string{"missing", "http", "socks5", "bad-socks", "bad-http", "conflicting", "session-without-socks"} {
		t.Run(connection, func(t *testing.T) {
			c := config.LoadConfig().HTTPClient
			c.Timeout = 200 * time.Millisecond
			c.MaxRetries = 0
			c.RequireProxy = true
			switch connection {
			case "http":
				c.HTTPProxyUrl = "http://" + address
			case "socks5":
				c.Socks5Url = "socks5h://" + address
			case "bad-socks":
				c.Socks5Url = "invalid"
			case "bad-http":
				c.HTTPProxyUrl = "ftp://" + address
			case "conflicting":
				c.Socks5Url = "socks5://" + address
				c.HTTPProxyUrl = "http://" + address
			case "session-without-socks":
				c.IsolateProxySessions = true
			}
			client := NewHTTPClientWithConfig(c)
			defer client.(interface{ Close() error }).Close()
			response, err := client.Perform(origin.URL)
			if response != nil {
				response.Body.Close()
			}
			if err == nil {
				t.Fatal("proxy failure succeeded")
			}
		})
	}
	if direct.Load() != 0 {
		t.Fatal("proxy failure made a direct request")
	}
}
func TestHostnameRestrictionIsAppliedToRedirects(t *testing.T) {
	var outside atomic.Int64
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { outside.Add(1) }))
	defer blocked.Close()
	allowed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://localhost:"+mustPort(blocked.URL)+"/outside", 302)
	}))
	defer allowed.Close()
	c := config.LoadConfig().HTTPClient
	c.AllowedHostSuffixes = []string{"127.0.0.1"}
	c.MaxRetries = 0
	client := NewHTTPClientWithConfig(c)
	defer client.(interface{ Close() error }).Close()
	res, err := client.Perform(allowed.URL)
	if res != nil {
		res.Body.Close()
	}
	if err == nil {
		t.Fatal("redirect escaped hostname policy")
	}
	if outside.Load() != 0 {
		t.Fatal("outside redirect was contacted")
	}
}
func mustPort(raw string) string { u, _ := url.Parse(raw); return u.Port() }
func TestRetryBackoffAndCancellationAreConfigurable(t *testing.T) {
	var calls atomic.Int64
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) }))
	defer site.Close()
	c := config.LoadConfig().HTTPClient
	c.MaxRetries = 5
	c.RetryDelay = time.Hour
	c.RetryMaxDelay = 2 * time.Hour
	client := NewHTTPClientWithConfig(c)
	defer client.(interface{ Close() error }).Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	_, err := client.(interface {
		PerformContext(context.Context, string) (*http.Response, error)
	}).PerformContext(ctx, site.URL)
	if err == nil || calls.Load() != 1 {
		t.Fatal("retry wait ignored context or backoff", err, calls.Load())
	}
	c.MaxRetries = 0
	client = NewHTTPClientWithConfig(c)
	defer client.(interface{ Close() error }).Close()
	response, err := client.Perform(site.URL)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if calls.Load() != 2 {
		t.Fatal("zero retries was not honored")
	}
}
