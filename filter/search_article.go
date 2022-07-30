package filter

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/iakinsey/delver/types/rpc"
	"github.com/pkg/errors"
)

type articleSearchFilter struct {
	rpc.FilterParams
	rpc.ArticleFilterQuery

	Must            []interface{}
	FieldConstraint []string
	Complete        bool
}

const defaultDaysLookback = 90

var defaultFields = []string{"title", "url", "url_md5", "found"}

const queryTemplate = `{
    "from": 0,
    "size": 10000,
    "sort": [
        {"found": {"order": "desc"}}
    ],
    "query": {
        "bool": {
            "must": %s
        }
	}
}`
const dateRangeTemplate = `{
	"range": {"found": {"gte": %d}}
}`
const countryTemplate = `
	{"match": {"countries": {"query": "%s"}}}
`
const keywordTemplate = `
	{"query": "%s", "fields": ["summary", "content", "title"]}
`
const companyTemplate = `
	{"match": {"corporate": {"query": "%s"}}}
`

func NewArticleSearchFilter(params rpc.FilterParams) SearchFilter {
	articleFilter, ok := params.Query.(rpc.ArticleFilterQuery)

	if !ok {
		log.Fatalf("failed to cast to article filter")
	}

	return &articleSearchFilter{
		FilterParams:       params,
		ArticleFilterQuery: articleFilter,
		Must:               make([]interface{}, 0),
		FieldConstraint:    make([]string, 0),
		Complete:           false,
	}
}

func (s *articleSearchFilter) Perform() (io.Reader, error) {
	if s.Complete {
		return s.buildQuery()
	}

	s.transformDateRange()
	s.transformFields()
	s.transformCountry()
	s.transformKeyword()
	s.transformCompany()

	s.Complete = true

	return s.buildQuery()
}

func (s *articleSearchFilter) transformDateRange() {
	daysLookback := s.Range

	if daysLookback == 0 {
		daysLookback = defaultDaysLookback
	}
	lookback := time.Now().AddDate(0, 0, -daysLookback).Unix()
	part := json.RawMessage(fmt.Sprintf(dateRangeTemplate, lookback))

	s.Must = append(s.Must, part)
}
func (s *articleSearchFilter) transformFields() {
	fields := s.Fields

	if len(fields) == 0 {
		fields = defaultFields
	}

	s.FieldConstraint = fields
}

func (s *articleSearchFilter) transformCountry() {
	if len(s.Country) == 0 {
		return
	}

	for _, country := range s.Country {
		q := json.RawMessage(fmt.Sprintf(countryTemplate, country))
		s.Must = append(s.Must, q)
	}
}

func (s *articleSearchFilter) transformKeyword() {
	if len(s.Keyword) == 0 {
		return
	}

	var part map[string][]json.RawMessage = make(map[string][]json.RawMessage)
	part["multi_match"] = make([]json.RawMessage, 0)

	for _, keyword := range s.Keyword {
		q := json.RawMessage(fmt.Sprintf(keywordTemplate, keyword))
		part["multi_match"] = append(part["multi_match"], q)
	}

	s.Must = append(s.Must, part)
}

func (s *articleSearchFilter) transformCompany() {
	if len(s.Company) == 0 {
		return
	}

	for _, company := range s.Company {
		q := json.RawMessage(fmt.Sprintf(companyTemplate, company))
		s.Must = append(s.Must, q)
	}
}

func (s *articleSearchFilter) buildQuery() (io.Reader, error) {
	b, err := json.Marshal(s.Must)

	if err != nil {
		return nil, errors.Wrap(err, "failed to serialize json when building article search query")
	}

	query := fmt.Sprintf(queryTemplate, string(b))

	return strings.NewReader(query), nil
}