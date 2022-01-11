package extractors

import (
	"fmt"
	"testing"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/assert"
)

func prepareAdvTest(origin string, uris []string) (interface{}, error) {
	extractor := NewAdversarialExtractor()

	meta := message.FetcherResponse{
		FetcherRequest: message.FetcherRequest{
			URI: origin,
		},
	}

	inputUris := features.URIs(uris)

	composite := types.CompositeAnalysis{
		URIs: &inputUris,
	}

	return extractor.Perform(nil, meta, composite)
}

func TestAdversarialExtractorIsEnumeration(t *testing.T) {
	adv, err := prepareAdvTest("http://example.com", []string{
		"http://examplf.com",
		"http://examplg.com",
	})

	assert.NoError(t, err)
	assert.NotNil(t, adv)
	assert.IsType(t, features.Adversarial{}, adv)
	assert.True(t, *adv.(features.Adversarial).Enumeration)
}

func TestAdversarialExtractorIsSubdomainExplosion(t *testing.T) {
	var explodedSubdomains []string

	for i := 0; i < subdomainThreshold; i++ {
		url := fmt.Sprintf("http://test%c.example.com", rune(i+65))
		explodedSubdomains = append(explodedSubdomains, url)
	}

	adv, err := prepareAdvTest("http://example.com", explodedSubdomains)

	assert.NoError(t, err)
	assert.NotNil(t, adv)
	assert.IsType(t, features.Adversarial{}, adv)
	assert.True(t, *adv.(features.Adversarial).SubdomainExplosion)
}

func TestAdversarialExtractorNotAdversarial(t *testing.T) {
}
