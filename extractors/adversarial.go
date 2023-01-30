package extractors

import (
	"net/url"
	"os"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
)

type adversarialExtractor struct {
	config.AdversarialConfig
}

// TODO turn these into matrix operations
func NewAdversarialExtractor() Extractor {
	conf := config.Get()

	return &adversarialExtractor{
		conf.Adversarial,
	}
}

func (s *adversarialExtractor) Perform(f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	var uris features.URIs

	if err := composite.Load(features.UrlField, &uris); err != nil {
		return nil, err
	}

	origin, err := url.Parse(composite.URI)

	if err != nil {
		return nil, err
	}

	var urls []*url.URL

	for _, u := range uris {
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
	return features.AdversarialField
}

func (s *adversarialExtractor) Requires() []string {
	return []string{
		features.UrlField,
	}
}

func (s *adversarialExtractor) detectEnumeration(urls []*url.URL) bool {
	counter := 0

	for _, u1 := range urls {
		d1 := util.GetSLD(u1.Host)

		if d1 == "" {
			continue
		}

		for _, u2 := range urls {
			if u1 == u2 {
				continue
			}

			d2 := util.GetSLD(u2.Host)

			if d2 == "" {
				continue
			}

			if d1[len(d1)-1] != d2[len(d2)-1]+1 {
				continue
			}

			counter += 1

			if counter >= int(s.EnumerationThreshold) {
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

		if counter >= int(s.SubdomainThreshold) {
			return true
		}

		keys[sld2] = true
	}

	return false
}
