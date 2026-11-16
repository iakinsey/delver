package fetcher

import (
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProxyEndpointFailureSeparatedFromDestinationFailure(t *testing.T) {
	for _, tc := range []struct{ text, kind string }{
		{"proxy connection: dial tcp 127.0.0.1:9050: connection refused", "proxy"},
		{"proxy connection: i/o timeout", "proxy"},
		{"proxyconnect tcp: context deadline exceeded", "proxy"},
		{"socks connect tcp 127.0.0.1:9050->service.onion:80: host unreachable", "connection"},
		{"socks connect tcp 127.0.0.1:9050->service.onion:80: context deadline exceeded", "timeout"},
	} {
		r := message.FetcherResponse{Error: tc.text}
		classifyResponse(&r)
		require.Equal(t, tc.kind, r.FailureClass)
		require.True(t, r.Retryable)
	}
}
