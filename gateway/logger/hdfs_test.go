package logger

import (
	"testing"
	"time"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/assert"
)

func TestHDFSLogger(t *testing.T) {
	namenode := "localhost:9000"
	logger := NewHDFSLogger(namenode)
	adv := true
	tu := int32(44)
	composite := message.CompositeAnalysis{
		RequestID:     types.NewV4(),
		URI:           "http://esxample.com",
		Host:          "example.com",
		Origin:        "exampleold",
		Protocol:      types.ProtocolHTTP,
		StoreKey:      types.NewV4(),
		ContentMD5:    "an-example-md5",
		ElapsedTimeMs: 123,
		Error:         "no error",
		HTTPCode:      404,
		Success:       true,
		Timestamp:     time.Now().Unix(),
		Header: map[string][]string{
			"test1": {"test2"},
			"test3": {"test4"},
			"test5": {"test6"},
		},
		Adversarial: &features.Adversarial{
			Enumeration:        &adv,
			SubdomainExplosion: &adv,
		},
		Corporations: []string{
			"test1",
			"test2",
		},
		Countries: []string{
			"1",
			"2",
		},
		Language: &features.Language{
			Name:       features.LangEnglish,
			Confidence: 0.123,
		},
		TextContent: "Test me 1234",
		Sentiment: &features.Sentiment{
			BinaryNaiveBayesContent: &tu,
		},
		URIs: []string{
			"test-string",
		},
	}

	assert.NoError(t, logger.LogResource(composite))
}
