package logger

import (
	"testing"

	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/assert"
)

const enableEstest = false

func TestElasticsearchLogger(t *testing.T) {
	if !enableEstest {
		return
	}

	l := NewElasticsearchLogger([]string{"http://localhost:9200"})
	composite := message.CompositeAnalysis{
		TextContent: "Test text content",
		Corporations: []string{
			"example1",
			"example2",
		},
		FetcherResponse: message.FetcherResponse{
			HTTPCode: 200,
			FetcherRequest: message.FetcherRequest{
				URI:  "http://example.com",
				Host: "example.com",
			},
		},
	}
	err := l.LogResource(composite)

	assert.NoError(t, err)
}
