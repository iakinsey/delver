package filter

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/iakinsey/delver/types/rpc"
	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
)

type compositeSearchFilter struct {
	rpc.FilterParams
	rpc.CompositeFilterQuery

	Must            []interface{}
	FieldConstraint string
	Complete        bool
}

const dateRangeTemplate = `{
	"range": {"timestamp": {"gte": %d}}
}`
const countryTemplate = `
	{"match": {"features.country": {"query": "%s"}}}
`
const keywordTemplate = `
	{"multi_match": {"query": "%s", "fields": ["features.text", "features.title"]}}
`
const companyTemplate = `
	{"match": {"features.company_name": {"query": "%s"}}}
`

func NewCompositeSearchFilter(params rpc.FilterParams) SearchFilter {
	compositeFilter := params.Query.(rpc.CompositeFilterQuery)

	return &compositeSearchFilter{
		FilterParams:         params,
		CompositeFilterQuery: compositeFilter,
		Must:                 make([]interface{}, 0),
		Complete:             false,
	}
}

func (s *compositeSearchFilter) IsAggregate() bool {
	return false
}

func (s *compositeSearchFilter) Perform() (io.Reader, error) {
	if s.Complete {
		return s.buildQuery()
	}

	s.transformCountry()
	s.transformKeyword()
	s.transformCompany()
	s.transformDateRange()
	s.transformFields()
	s.transformMatchString("url", s.Url)
	s.transformMatchString("domain", s.Domain)
	s.transformMatchString("title", s.Title)
	s.transformMatchString("language", s.Language)
	s.transformMatchInt("http_code", s.HttpCode)

	s.Complete = true

	return s.buildQuery()
}

func (s *compositeSearchFilter) Postprocess(entities []json.RawMessage) ([]json.RawMessage, error) {
	return entities, nil
}

func (s *compositeSearchFilter) transformMatchString(key string, vals []string) {
	for _, val := range vals {
		q := `{"match": {"%s": {"query": "%s"}}}`
		s.Must = append(s.Must, json.RawMessage(fmt.Sprintf(q, key, val)))
	}
}

func (s *compositeSearchFilter) transformMatchInt(key string, vals []int) {
	for _, val := range vals {
		q := `{"match": {"%s": {"query": "%d"}}}`
		s.Must = append(s.Must, json.RawMessage(fmt.Sprintf(q, key, val)))
	}
}

func (s *compositeSearchFilter) transformDateRange() {
	daysLookback := s.Range

	if daysLookback == 0 {
		daysLookback = compositeDefaultDaysLookback
	}
	lookback := time.Now().AddDate(0, 0, -daysLookback).Unix()
	part := json.RawMessage(fmt.Sprintf(dateRangeTemplate, lookback))

	s.Must = append(s.Must, part)
}
func (s *compositeSearchFilter) transformFields() {
	fields := s.Fields

	if len(fields) == 0 {
		fields = compositeDefaultFields
	}

	s.FieldConstraint = util.ToEscapedStringList(fields)
}

func (s *compositeSearchFilter) transformCountry() {
	if len(s.Country) == 0 {
		return
	}

	for _, country := range s.Country {
		q := json.RawMessage(fmt.Sprintf(countryTemplate, country))
		s.Must = append(s.Must, q)
	}
}

func (s *compositeSearchFilter) transformKeyword() {
	if len(s.Keyword) == 0 {
		return
	}

	for _, keyword := range s.Keyword {
		q := json.RawMessage(fmt.Sprintf(keywordTemplate, keyword))
		s.Must = append(s.Must, q)
	}
}

func (s *compositeSearchFilter) transformCompany() {
	if len(s.Company) == 0 {
		return
	}

	for _, company := range s.Company {
		q := json.RawMessage(fmt.Sprintf(companyTemplate, company))
		s.Must = append(s.Must, q)
	}
}

func (s *compositeSearchFilter) buildQuery() (io.Reader, error) {
	b, err := json.Marshal(s.Must)

	if err != nil {
		return nil, errors.Wrap(err, "failed to serialize json when building composite search query")
	}

	query := fmt.Sprintf(queryTemplate, s.FieldConstraint, string(b))

	return strings.NewReader(query), nil
}
