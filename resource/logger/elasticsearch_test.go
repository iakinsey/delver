package logger

import (
	"net/http"
	"testing"

	"github.com/elastic/go-elasticsearch"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/assert"
)

const mockConnection = true

type mockRoundTripper struct{}

func (s *mockRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{}, nil
}

func TestElasticsearchLogger(t *testing.T) {
	var l Logger

	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: make([]string, 1),
		Transport: &mockRoundTripper{},
	})

	assert.NoError(t, err)

	if mockConnection {
		l = &elasticsearchLogger{
			index:  "resource",
			client: client,
		}
	} else {
		l = NewElasticsearchLogger(ElasticsearchLoggerParams{
			Addresses: []string{"http://localhost:9200"},
		})
	}

	composite := message.CompositeAnalysis{
		FetcherResponse: message.FetcherResponse{
			FetcherRequest: message.FetcherRequest{
				URI:  "http://example.com",
				Host: "example.com",
			},
			HTTPCode: 200,
		},
		TextContent: "Test text content",
		Corporations: []string{
			"example1",
			"example2",
		},
	}
	err = l.LogResource(composite)

	assert.NoError(t, err)
}
