package extractors

import (
	"os"

	log "github.com/sirupsen/logrus"

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
		log.Fatalf(err.Error())
	}

	return &countryExtractor{
		countries: countries,
	}
}

func (s *countryExtractor) Perform(f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	var results []string
	var textContent string

	if err := composite.Load(message.TextExtractor, &textContent); err != nil {
		return nil, err
	}

	for iso3166Alpha2, regex := range s.countries {
		if r := regex.Find([]byte(textContent)); r != nil {
			results = append(results, iso3166Alpha2)
		}
	}

	return features.Countries(util.DedupeStrSlice(results)), nil
}

func (s *countryExtractor) Name() string {
	return message.CountryExtractor
}

func (s *countryExtractor) Requires() []string {
	return []string{
		message.TextExtractor,
	}
}
