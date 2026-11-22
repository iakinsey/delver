package publisher

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/types"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestRSSProtectionInspectsBeforePublishingItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>synthetic restricted marker</title><link>http://fixture.test/</link><description>Harmless test</description><item><title>Item</title><link>http://unvisited.test/page</link></item></channel></rss>`)
	}))
	defer srv.Close()
	p, e := protection.Open(filepath.Join(t.TempDir(), "policy.sqlite"))
	require.NoError(t, e)
	defer p.Close()
	require.NoError(t, p.Update(1, nil, []protection.Rule{{ID: "synthetic", Enabled: true, Phrases: []string{"synthetic restricted marker"}}}))
	c := config.LoadConfig()
	c.RuntimeCrawlID = "crawl"
	c.RuntimeRunID = "run"
	c.HTTPClient.Protection = p
	c.HTTPClient.RespectRobots = false
	c.HTTPClient.Timeout = time.Second
	source := NewRssFeedPublisherWithConfig(RssFeedPublisherParams{Uris: []string{srv.URL}, MaxConcurrency: 1}, c)
	defer source.OnComplete()
	out, e := source.(interface {
		OnMessageContext(context.Context, types.Message) (interface{}, error)
	}).OnMessageContext(context.Background(), types.Message{})
	require.NoError(t, e)
	raw, e := json.Marshal(out)
	require.NoError(t, e)
	require.NotContains(t, string(raw), "unvisited.test")
	incidents := p.PublicSnapshot().Incidents
	require.Len(t, incidents, 1)
	require.Equal(t, "crawl", incidents[0].CrawlID)
	require.Equal(t, "run", incidents[0].RunID)
	require.Empty(t, source.(interface{ Health() string }).Health())
}
