package extractors

import (
	"log"
	"os"

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
	countries, err := types.GetCountryRegexes(util.DataFilePath(countriesFileName))

	if err != nil {
		log.Fatalf(err.Error())
	}

	return &countryExtractor{
		countries: countries,
	}
}

func (s *countryExtractor) Perform(f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	var results []string

	for iso3166Alpha2, regex := range s.countries {
		if r := regex.Find(composite.TextContent); r != nil {
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
