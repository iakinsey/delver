package extractors

import (
	"context"
	"os"
	"strings"

	"github.com/pkg/errors"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
)

const countriesFileName = "countries.json"

type countryExtractor struct {
	countries types.CountryRegexes
}

func NewCountryExtractor() Extractor {
	conf := config.Get()
	countries, err := types.GetCountryRegexes(conf.CountriesPath)

	if err != nil {
		panic(err)
	}

	return &countryExtractor{
		countries: countries,
	}
}

func (s *countryExtractor) Perform(f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	return s.PerformContext(context.Background(), f, composite)
}
func (s *countryExtractor) PerformContext(ctx context.Context, f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var results []string
	var textContent string

	if err := composite.Load(features.TextField, &textContent); err != nil {
		return nil, errors.Wrap(err, "country extractor")
	}

	for iso3166Alpha3, regex := range s.countries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r := regex.FindReaderIndex(contextRuneReader{ctx, strings.NewReader(textContent)}); r != nil {
			results = append(results, iso3166Alpha3)
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return features.Countries(util.DedupeStrSlice(results)), nil
}

func (s *countryExtractor) Name() string {
	return features.CountryField
}

func (s *countryExtractor) Requires() []string {
	return []string{
		features.TextField,
	}
}
