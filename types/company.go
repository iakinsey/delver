package types

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"unicode"
)

type Company struct {
	CleanName  string `json:"clean_name"`
	Exchange   string `json:"exchange"`
	FormalName string `json:"formal_name"`
	Identifier string `json:"identifier"`
	Industry   string `json:"industry"`
	Sector     string `json:"sector"`
	Ticker     string `json:"ticker"`
	Regex      regexp.Regexp
}

var regexCharRange = []*unicode.RangeTable{
	unicode.Letter,
	unicode.Digit,
	unicode.Space,
}

func GetCompanies(path string) ([]*Company, error) {
	f, err := os.Open(path)

	if err != nil {
		return nil, err
	}

	defer f.Close()

	bytes, err := io.ReadAll(f)

	if err != nil {
		return nil, err
	}

	var companies []*Company

	if err = json.Unmarshal(bytes, &companies); err != nil {
		return nil, err
	}

	for _, company := range companies {
		if regex, err := GetCompanyRegex(company.CleanName); err != nil {
			return nil, err
		} else {
			company.Regex = *regex
		}
	}

	return companies, nil
}

func GetCompanyRegex(input string) (*regexp.Regexp, error) {
	if input == "" {
		return nil, errors.New("company name is empty")
	}

	var buffer bytes.Buffer

	for _, c := range input {
		if !unicode.IsOneOf(regexCharRange, c) {
			buffer.WriteRune('\\')
		}

		buffer.WriteRune(c)
	}

	return regexp.Compile(buffer.String())
}
