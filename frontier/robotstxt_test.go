package frontier

import (
	"context"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/util"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryRobots(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "User-agent: *\nDisallow: /bad\nDisallow: /alsobad\n")
	}))
	defer server.Close()
	filter := NewMemoryRobots()
	defer filter.Close()
	for path, expected := range map[string]bool{"/good": true, "/alsogood": true, "/bad": false, "/alsobad": false} {
		allowed, err := filter.IsAllowed(server.URL + path)
		if err != nil || allowed != expected {
			t.Fatalf("%s: allowed=%v error=%v", path, allowed, err)
		}
	}
}
func TestRobotsExpiration(t *testing.T) {
	filter := NewMemoryRobots().(*memoryRobots)
	defer filter.Close()
	filter.expiration = time.Second
	parsed, _ := url.Parse("https://example.test/page")
	key := "https://example.test"
	filter.mapMutex.Lock()
	filter.robotsMap[key] = robotsInfo{created: time.Now().Add(-time.Hour)}
	filter.mapMutex.Unlock()
	if info := filter.getRobots(parsed); info != nil {
		t.Fatal("stale rules returned")
	}
	filter.mapMutex.Lock()
	filter.robotsMap[key] = robotsInfo{created: time.Now()}
	filter.mapMutex.Unlock()
	if info := filter.getRobots(parsed); info == nil {
		t.Fatal("fresh rules discarded")
	}
	other, _ := url.Parse("http://example.test/page")
	if filter.getRobots(other) != nil {
		t.Fatal("cache merged different schemes")
	}
}
func TestRobotsRetrievalErrorDenied(t *testing.T) {
	filter := NewMemoryRobots().(*memoryRobots)
	defer filter.Close()
	filter.client = &util.MockDelverHTTPClient{Error: fmt.Errorf("offline")}
	for i := 0; i < 2; i++ {
		allowed, err := filter.IsAllowed("https://example.test/private")
		if err == nil || allowed {
			t.Fatal("robots failure allowed request")
		}
	}
}

func TestRobotsCanceledCacheWait(t *testing.T) {
	filter := NewMemoryRobots().(*memoryRobots)
	defer filter.Close()
	filter.fetchGate <- struct{}{}
	defer func() { <-filter.fetchGate }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if allowed, err := filter.IsAllowedContext(ctx, "https://example.test/private"); err == nil || allowed {
		t.Fatal("canceled robots lookup allowed request")
	}
}

func TestRobotsMetadataIgnoresPagePatternAndCancelsNetwork(t *testing.T) {
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			select {
			case started <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			return
		}
		t.Error("unexpected page request")
	}))
	defer server.Close()
	filter := NewMemoryRobots().(*memoryRobots)
	defer filter.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = util.WithHTTPPolicy(ctx, func(*url.URL) error { return fmt.Errorf("page pattern excludes robots") })
	done := make(chan error, 1)
	go func() { _, err := filter.IsAllowedContext(ctx, server.URL+"/article"); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("page policy blocked robots metadata")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancellation lost")
		}
	case <-time.After(time.Second):
		t.Fatal("robots request ignored cancellation")
	}
	if info := filter.getRobots(mustRobotsURL(server.URL)); info != nil {
		t.Fatal("cancellation poisoned robots cache")
	}
}
func mustRobotsURL(raw string) *url.URL { u, _ := url.Parse(raw); return u }

func TestRobotsPolicyFetchIsNotBlockedByDocumentExtensions(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/robots.txt" {
			t.Errorf("unexpected policy request %s", r.URL.Path)
		}
		fmt.Fprint(w, "User-agent: *\nDisallow: /denied\n")
	}))
	defer server.Close()
	c := config.LoadConfig()
	c.HTTPClient.ExcludedExtensions = []string{"txt"}
	filter := NewMemoryRobotsWithConfig(c)
	defer filter.Close()
	allowed, err := filter.IsAllowed(server.URL + "/page.html")
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = filter.IsAllowed(server.URL + "/denied")
	require.NoError(t, err)
	require.False(t, allowed)
	require.EqualValues(t, 1, requests.Load())
	client := util.NewHTTPClientWithConfig(c.HTTPClient)
	_, err = client.Perform(server.URL + "/document.txt")
	require.Error(t, err)
	require.EqualValues(t, 1, requests.Load(), "ordinary txt documents bypassed exclusions")
}
