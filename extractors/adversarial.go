package extractors

import (
	"net/url"
	"os"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
)

const subdomainThreshold = 25
const enumerationThreshold = 1

type adversarialExtractor struct {
	subdomainThreshold   int32
	enumerationThreshold int32
}

// TODO turn these into matrix operations
func NewAdversarialExtractor() Extractor {
	return &adversarialExtractor{
		subdomainThreshold:   subdomainThreshold,
		enumerationThreshold: enumerationThreshold,
	}
}

func (s *adversarialExtractor) Perform(f *os.File, meta message.FetcherResponse, composite types.CompositeAnalysis) (interface{}, error) {
	origin, err := url.Parse(meta.URI)

	if err != nil {
		return nil, err
	}

	var urls []*url.URL

	for _, u := range composite.URIs {
		u1, err := url.Parse(u)

		if err == nil && u1.Host != "" {
			urls = append(urls, u1)
		}
	}

	enumeration := s.detectEnumeration(urls)
	subdomainExplosion := s.detectSubdomainExplosion(origin, urls)

	return features.Adversarial{
		Enumeration:        &enumeration,
		SubdomainExplosion: &subdomainExplosion,
	}, nil
}

func (s *adversarialExtractor) Name() string {
	return types.AdversarialExtractor
}

func (s *adversarialExtractor) Requires() []string {
	return []string{
		types.UrlExtractor,
	}
}

func (s *adversarialExtractor) detectEnumeration(urls []*url.URL) bool {
	counter := 0

	for _, u1 := range urls {
		d1 := util.GetSLD(u1.Host)

		for _, u2 := range urls {
			if u1 == u2 {
				continue
			}

			d2 := util.GetSLD(u2.Host)

			if d1[len(d1)-1] != d2[len(d2)-1]+1 {
				continue
			}

			counter += 1

			if counter >= int(s.enumerationThreshold) {
				return true
			}
		}
	}

	return false
}

func (s *adversarialExtractor) detectSubdomainExplosion(origin *url.URL, urls []*url.URL) bool {
	sld1 := util.GetSLD(origin.Host)
	counter := 0
	keys := make(map[string]bool)

	for _, target := range urls {
		sld2 := util.GetSLD(target.Host)

		// Deduplicate on SLD
		if _, value := keys[target.Host]; value || sld1 != sld2 || origin.Host == target.Host {
			continue
		}

		counter += 1

		if counter >= int(s.subdomainThreshold) {
			return true
		}

		keys[sld2] = true
	}

	return false
}
