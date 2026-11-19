package publisher

import (
	"context"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/types"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestExcludedFeedIsSkippedWithoutSourceFailure(t *testing.T) {
	var blocked atomic.Int64
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			fmt.Fprint(w, "User-agent: *\nAllow: /\n")
		case "/good":
			w.Header().Set("Content-Type", "application/rss+xml")
			fmt.Fprint(w, `<rss version="2.0"><channel><title>Fixture</title><link>http://example.test/</link><description>Test</description><item><title>Article</title><link>http://example.test/article</link></item></channel></rss>`)
		default:
			blocked.Add(1)
		}
	}))
	defer site.Close()
	controller, err := flow.New(context.Background(), t.Name(), flow.Default(), nil)
	require.NoError(t, err)
	defer controller.Close()
	c := config.LoadConfig()
	c.HTTPClient.Flow = controller
	c.HTTPClient.ExcludedExtensions = []string{"txt", "xml"}
	c.HTTPClient.RespectRobots = true
	p := NewRssFeedPublisherWithConfig(RssFeedPublisherParams{Uris: []string{site.URL + "/good", site.URL + "/blocked.xml"}}, c).(*rssFeedPublisher)
	defer p.OnComplete()
	out, err := p.OnMessage(types.Message{})
	require.NoError(t, err)
	require.Len(t, out.(types.MultiMessage).Values, 1)
	require.Empty(t, p.Health())
	require.Zero(t, blocked.Load())
	require.EqualValues(t, 1, controller.Snapshot().Counters["feed_policy_skips"])
}
