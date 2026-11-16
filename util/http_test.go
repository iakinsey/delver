package util

import (
	"context"
	"fmt"
	"github.com/iakinsey/delver/config"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestHTTPRetryAndCancellation(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, "ok")
	}))
	defer server.Close()
	client := &delverHTTPClient{HTTP: server.Client(), MaxRetries: 1, Concurrency: 1}
	response, err := client.Perform(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if calls != 2 || response.StatusCode != 200 {
		t.Fatal("retry failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = client.PerformContext(ctx, server.URL); err == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestHTTPRedirectPolicy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("disallowed redirect fetched") }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer server.Close()
	client := NewHTTPClient().(*delverHTTPClient)
	client.HTTP.Timeout = time.Second
	allowed := server.URL
	SetHTTPPolicy(client, func(u *url.URL) error {
		if u.Scheme+"://"+u.Host != allowed {
			return fmt.Errorf("outside scope")
		}
		return nil
	})
	if response, err := client.Perform(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("redirect policy ignored")
	}
}
func TestHTTPDefaults(t *testing.T) {
	if config.Get().HTTPClient.MaxResponseBytes <= 0 {
		t.Fatal("missing size bound")
	}
}

func TestManagedPacingDoesNotConsumeNetworkTimeout(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	defer site.Close()
	c := config.LoadConfig().HTTPClient
	c.Timeout = 60 * time.Millisecond
	c.GlobalGate = NewOriginGate()
	c.LocalGate = NewOriginGate()
	c.MaxRetries = 0
	client := NewHTTPClientWithConfig(c)
	for i := 0; i < 2; i++ {
		response, err := client.Perform(site.URL)
		if err != nil {
			t.Fatal("pacing wait consumed the network timeout", err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
	}
}
