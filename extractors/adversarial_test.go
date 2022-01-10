package extractors

import (
	"testing"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/assert"
)

func TestAdversarialExtractorIsEnumeration(t *testing.T) {
	extractor := NewAdversarialExtractor()

	meta := message.FetcherResponse{
		FetcherRequest: message.FetcherRequest{
			URI: "http://example.com",
		},
	}

	uris := features.URIs([]string{
		"http://examplf.com",
		"http://examplg.com",
	})

	composite := types.CompositeAnalysis{
		URIs: &uris,
	}

	adv, err := extractor.Perform(nil, meta, composite)

	assert.NoError(t, err)
	assert.NotNil(t, adv)
	assert.IsType(t, features.Adversarial{}, adv)

	assert.True(t, *adv.(features.Adversarial).Enumeration)
}

func TestAdversarialExtractorIsSubdomainExplosion(t *testing.T) {
}

func TestAdversarialExtractorNotAdversarial(t *testing.T) {
}
