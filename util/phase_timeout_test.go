package util

import (
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/stretchr/testify/require"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func phaseClient() config.HTTPClientConfig {
	c := config.LoadConfig().HTTPClient
	c.Timeout = time.Second
	c.MaxRetries = 0
	c.RequestDelay = 0
	c.GlobalGate = NewOriginGate()
	c.LocalGate = NewOriginGate()
	return c
}
func TestHeaderAndBodyPhaseTimeouts(t *testing.T) {
	for _, body := range []bool{false, true} {
		t.Run(fmt.Sprint(body), func(t *testing.T) {
			site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if body {
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
				}
				select {
				case <-r.Context().Done():
					return
				case <-time.After(200 * time.Millisecond):
					fmt.Fprint(w, "late")
				}
			}))
			defer site.Close()
			c := phaseClient()
			if body {
				c.BodyTimeout = 30 * time.Millisecond
			} else {
				c.HeaderTimeout = 30 * time.Millisecond
			}
			client := NewHTTPClientWithConfig(c)
			started := time.Now()
			response, err := client.Perform(site.URL)
			if body {
				require.NoError(t, err)
				_, err = io.ReadAll(response.Body)
				response.Body.Close()
			}
			require.Error(t, err)
			require.Less(t, time.Since(started), 500*time.Millisecond)
		})
	}
}
func TestConnectTimeoutIncludesSOCKSHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		conn, e := listener.Accept()
		if e == nil {
			defer conn.Close()
			<-done
		}
	}()
	c := phaseClient()
	c.Socks5Url = "socks5://" + listener.Addr().String()
	c.ConnectTimeout = 30 * time.Millisecond
	started := time.Now()
	_, err = NewHTTPClientWithConfig(c).Perform("http://remote.invalid/")
	require.Error(t, err)
	require.Less(t, time.Since(started), 500*time.Millisecond)
}
func TestOverallBudgetSharedAcrossRedirects(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(45 * time.Millisecond):
		}
		http.Redirect(w, r, r.URL.Path+"x", 302)
	}))
	defer site.Close()
	c := phaseClient()
	c.Timeout = 80 * time.Millisecond
	started := time.Now()
	_, err := NewHTTPClientWithConfig(c).Perform(site.URL)
	require.Error(t, err)
	require.Less(t, time.Since(started), 300*time.Millisecond)
}

func TestTLSHandshakeTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		conn, e := listener.Accept()
		if e == nil {
			defer conn.Close()
			<-done
		}
	}()
	c := phaseClient()
	c.TLSTimeout = 30 * time.Millisecond
	started := time.Now()
	_, err = NewHTTPClientWithConfig(c).Perform("https://" + listener.Addr().String())
	require.Error(t, err)
	require.Less(t, time.Since(started), 500*time.Millisecond)
}
