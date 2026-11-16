package gateway

import (
	"context"
	"github.com/iakinsey/delver/types"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIndexCancellation(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"version":{"number":"7.17.1"},"tagline":"You Know, for Search"}`))
			return
		}
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	gateway := NewSearchGateway([]string{server.URL}).(*searchGateway)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := gateway.IndexManyContext(ctx, []*types.Indexable{{Index: "test", ID: "stable", Data: map[string]string{"title": "test"}}})
	require.Error(t, err)
}
