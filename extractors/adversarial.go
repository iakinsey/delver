package extractors

import (
	"context"
	"net/url"
	"os"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/trap"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
)

type adversarialExtractor struct {
	config.AdversarialConfig
}

func NewAdversarialExtractor() Extractor {
	return NewAdversarialExtractorWithConfig(config.Get())
}
func NewAdversarialExtractorWithConfig(conf config.Config) Extractor {

	return &adversarialExtractor{
		conf.Adversarial,
	}
}

func (s *adversarialExtractor) Perform(f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	return s.PerformContext(context.Background(), f, composite)
}
func (s *adversarialExtractor) PerformContext(ctx context.Context, f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var uris features.URIs

	if err := composite.Load(features.UrlField, &uris); err != nil {
		return nil, errors.Wrap(err, "adversarial extractor")
	}

	origin, err := url.Parse(composite.URI)

	if err != nil {
		return nil, err
	}

	var urls []*url.URL

	for _, u := range uris {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		u1, err := url.Parse(u)

		if err == nil && u1.Host != "" {
			urls = append(urls, u1)
		}
	}

	enumeration := s.detectEnumerationContext(ctx, urls)
	subdomainExplosion := s.detectSubdomainExplosionContext(ctx, origin, urls)

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var structure *trap.Observation
	if cache := prepared(ctx); cache != nil {
		cache.Lock()
		if cache.structure != nil && cache.uri == composite.URI {
			observation := cache.structure.Observation(composite.URI, uris)
			structure = &observation
		}
		cache.Unlock()
	}
	if structure == nil && f != nil {
		if _, err := f.Seek(0, 0); err != nil {
			return nil, err
		}
		observation, err := trap.Observe(ctx, f, composite.URI, uris)
		if err != nil {
			return nil, err
		}
		structure = &observation
	}

	return features.Adversarial{Structure: structure,
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
	return s.detectEnumerationContext(context.Background(), urls)
}
func (s *adversarialExtractor) detectEnumerationContext(ctx context.Context, urls []*url.URL) bool {
	var endings [256]int
	for _, u := range urls {
		if ctx.Err() != nil {
			return false
		}
		domain := util.GetSLD(u.Host)
		if domain != "" {
			endings[domain[len(domain)-1]]++
		}
	}
	counter := 0
	for _, u := range urls {
		if ctx.Err() != nil {
			return false
		}
		domain := util.GetSLD(u.Host)
		if domain == "" {
			continue
		}
		last := domain[len(domain)-1]
		counter += endings[last-1]
		if counter > 0 && counter >= s.EnumerationThreshold {
			return true
		}
	}
	return false
}

func (s *adversarialExtractor) detectSubdomainExplosion(origin *url.URL, urls []*url.URL) bool {
	return s.detectSubdomainExplosionContext(context.Background(), origin, urls)
}
func (s *adversarialExtractor) detectSubdomainExplosionContext(ctx context.Context, origin *url.URL, urls []*url.URL) bool {
	sld1 := util.GetSLD(origin.Host)
	counter := 0
	keys := make(map[string]bool)

	for _, target := range urls {
		if ctx.Err() != nil {
			return false
		}
		sld2 := util.GetSLD(target.Host)

		// Deduplicate on SLD
		if _, value := keys[target.Host]; value || sld1 != sld2 || origin.Host == target.Host {
			continue
		}

		counter += 1

		if counter >= int(s.SubdomainThreshold) {
			return true
		}

		keys[target.Host] = true
	}

	return false
}
