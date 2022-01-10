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

type adversarialExtractor struct {
	subdomainThreshold int32
}

func NewAdversarialExtractor() Extractor {
	return &adversarialExtractor{
		subdomainThreshold: subdomainThreshold,
	}
}

func (s *adversarialExtractor) Perform(f *os.File, meta message.FetcherResponse, composite types.CompositeAnalysis) (interface{}, error) {
	origin, err := url.Parse(meta.URI)

	if err != nil {
		return nil, err
	}

	var urls []*url.URL

	for _, u := range *composite.URIs {
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
	for _, u1 := range urls {
		for _, u2 := range urls {
			if u1.Host[len(u1.Host)-1] == u2.Host[len(u2.Host)-1]+1 {
				return true
			}
		}
	}

	return false
}

func (s *adversarialExtractor) detectSubdomainExplosion(origin *url.URL, urls []*url.URL) bool {
	sld1 := util.GetSecondLevelDomain(origin.String())
	counter := 0

	for _, target := range urls {
		sld2 := util.GetSecondLevelDomain(target.String())

		if sld1 != sld2 || origin.Host == target.Host {
			continue
		}

		counter += 1

		if counter >= int(s.subdomainThreshold) {
			return true
		}
	}

	return false
}
