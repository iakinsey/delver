package filter

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
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
	s.transformMatchString("uri", s.Url)
	s.transformMatchString("host", s.Domain)
	s.transformMatchString("features.title", s.Title)
	s.transformMatchString("features.language.name", s.Language)
	s.transformMatchInt("http_code", s.HttpCode)

	s.Complete = true

	return s.buildQuery()
}

func (s *compositeSearchFilter) Postprocess(entities []json.RawMessage) ([]json.RawMessage, error) {
	sort.SliceStable(entities, func(i, j int) bool {
		var a, b struct {
			Timestamp float64 `json:"timestamp"`
		}
		json.Unmarshal(entities[i], &a)
		json.Unmarshal(entities[j], &b)
		return a.Timestamp < b.Timestamp
	})
	return entities, nil
}

func (s *compositeSearchFilter) transformMatchString(key string, vals []string) {
	if len(vals) == 0 {
		return
	}
	clauses := []interface{}{}
	for _, v := range vals {
		field := key
		if key == "features.title" {
			field = "features.title.exact"
		}
		clauses = append(clauses, map[string]interface{}{"term": map[string]interface{}{field: v}})
	}
	s.Must = append(s.Must, map[string]interface{}{"bool": map[string]interface{}{"should": clauses, "minimum_should_match": 1}})
}
func (s *compositeSearchFilter) transformMatchInt(key string, vals []int) {
	if len(vals) > 0 {
		s.Must = append(s.Must, map[string]interface{}{"terms": map[string]interface{}{key: vals}})
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
		q := map[string]interface{}{"term": map[string]interface{}{"features.country": country}}
		s.Must = append(s.Must, q)
	}
}

func (s *compositeSearchFilter) transformKeyword() {
	if len(s.Keyword) == 0 {
		return
	}

	for _, keyword := range s.Keyword {
		escaped := strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?").Replace(keyword)
		clauses := []interface{}{}
		for _, field := range []string{"features.text.literal", "features.title.literal"} {
			clauses = append(clauses, map[string]interface{}{"wildcard": map[string]interface{}{field: map[string]interface{}{"value": "*" + escaped + "*", "case_insensitive": true}}})
		}
		q := map[string]interface{}{"bool": map[string]interface{}{"should": clauses, "minimum_should_match": 1}}

		s.Must = append(s.Must, q)
	}
}

func (s *compositeSearchFilter) transformCompany() {
	if len(s.Company) == 0 {
		return
	}

	for _, company := range s.Company {
		q := map[string]interface{}{"term": map[string]interface{}{"features.company_name": company}}
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
