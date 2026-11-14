package types

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

type Countries map[string][]string
type CountryRegexes map[string]*regexp.Regexp

func GetCountryRegexes(path string) (CountryRegexes, error) {
	countries, err := GetCountries(path)

	if err != nil {
		return nil, err
	}

	countryRegexes := make(CountryRegexes)

	for iso3166Alpha2, countryNames := range countries {
		pattern := fmt.Sprintf("\\b(?:%s)\\b", strings.Join(escapedCountryNames(countryNames), "|"))
		countryRegexes[iso3166Alpha2] = regexp.MustCompile(pattern)
	}

	return countryRegexes, nil
}

func GetCountries(path string) (Countries, error) {
	f, err := os.Open(path)

	if err != nil {
		return nil, err
	}

	defer f.Close()
	data, err := io.ReadAll(f)

	if err != nil {
		return nil, err
	}

	var countries Countries

	if err = json.Unmarshal(data, &countries); err != nil {
		return nil, err
	}

	return countries, nil
}

func escapedCountryNames(names []string) []string {
	result := make([]string, len(names))
	for i, name := range names {
		result[i] = regexp.QuoteMeta(name)
	}
	return result
}
