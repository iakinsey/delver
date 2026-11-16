package publisher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iakinsey/delver/types"
	"github.com/stretchr/testify/require"
)

type feedContextClient struct{ client *http.Client }

func (c feedContextClient) Perform(uri string) (*http.Response, error) {
	return c.PerformContext(context.Background(), uri)
}
func (c feedContextClient) PerformContext(ctx context.Context, uri string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	return c.client.Do(req)
}
func TestRSSCancellation(t *testing.T) {
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	publisher := &rssFeedPublisher{uris: []string{server.URL}, client: feedContextClient{server.Client()}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := publisher.OnMessageContext(ctx, types.Message{}); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("feed never started")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("feed did not cancel")
	}
}

func TestRSSPartialFailureHealthAndConcurrency(t *testing.T) {
	var active, maximum int32
	var fail int32 = 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&active, 1)
		defer atomic.AddInt32(&active, -1)
		for {
			old := atomic.LoadInt32(&maximum)
			if current <= old || atomic.CompareAndSwapInt32(&maximum, old, current) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		if r.URL.Path == "/bad" && atomic.LoadInt32(&fail) == 1 {
			w.WriteHeader(500)
			return
		}
		fmt.Fprintf(w, `<rss version="2.0"><channel><title>Feed</title><link>http://example.test/</link><description>Test</description><item><title>Article</title><link>http://example.test%s/article</link></item></channel></rss>`, r.URL.Path)
	}))
	defer server.Close()
	uris := []string{server.URL + "/bad"}
	for i := 0; i < 12; i++ {
		uris = append(uris, fmt.Sprintf("%s/feed%d", server.URL, i))
	}
	publisher := &rssFeedPublisher{uris: uris, concurrency: 2, client: feedContextClient{server.Client()}}
	result, err := publisher.OnMessageContext(context.Background(), types.Message{})
	require.NoError(t, err)
	require.Len(t, result.(types.MultiMessage).Values, 12)
	require.Contains(t, publisher.Health(), "HTTP 500")
	require.LessOrEqual(t, atomic.LoadInt32(&maximum), int32(2))
	atomic.StoreInt32(&fail, 0)
	result, err = publisher.OnMessageContext(context.Background(), types.Message{})
	require.NoError(t, err)
	require.Len(t, result.(types.MultiMessage).Values, 13)
	require.Empty(t, publisher.Health())
}
func TestRSSBodyLimit(t *testing.T) {
	reader := &feedBody{ctx: context.Background(), reader: strings.NewReader(strings.Repeat("a", 65)), remaining: 64}
	_, err := io.ReadAll(reader)
	require.Error(t, err)
	require.Contains(t, err.Error(), "response-size limit")
}
