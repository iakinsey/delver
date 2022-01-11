package extractors

import (
	"io/ioutil"
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

func (s *countryExtractor) Perform(f *os.File, meta message.FetcherResponse, composite types.CompositeAnalysis) (interface{}, error) {
	var results []string

	contents, err := ioutil.ReadAll(f)

	if err != nil {
		return nil, err
	}

	for iso3166Alpha2, regex := range s.countries {
		if r := regex.Find(contents); r != nil {
			results = append(results, iso3166Alpha2)
		}
	}

	return features.Countries(util.DedupeStrSlice(results)), nil
}

func (s *countryExtractor) Name() string {
	return types.CountryExtractor
}

func (s *countryExtractor) Requires() []string {
	return nil
}
