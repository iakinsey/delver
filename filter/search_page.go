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

type pageSearchFilter struct {
	rpc.FilterParams
	rpc.PageFilterQuery

	Must            []json.RawMessage
	FieldConstraint string
	Complete        bool
}

func NewPageSearchFilter(params rpc.FilterParams) SearchFilter {
	pageFilter := params.Query.(rpc.PageFilterQuery)

	return &pageSearchFilter{
		FilterParams:    params,
		PageFilterQuery: pageFilter,
		Must:            make([]json.RawMessage, 0),
		Complete:        false,
	}
}

func (s *pageSearchFilter) Perform() (io.Reader, error) {
	if s.Complete {
		return s.buildQuery()
	}

	s.transformMatchString("url", s.Url)
	s.transformMatchString("domain", s.Domain)
	s.transformMatchString("title", s.Title)
	s.transformMatchString("language", s.Language)
	s.transformMatchInt("http_code", s.HttpCode)
	s.transformFields()
	s.transformDateRange()

	s.Complete = true

	return s.buildQuery()
}

func (s *pageSearchFilter) transformMatchString(key string, vals []string) {
	for _, val := range vals {
		q := `{"match": {"%s": {"query": "%s"}}}`
		s.Must = append(s.Must, json.RawMessage(fmt.Sprintf(q, key, val)))
	}
}

func (s *pageSearchFilter) transformMatchInt(key string, vals []int) {
	for _, val := range vals {
		q := `{"match": {"%s": {"query": "%d"}}}`
		s.Must = append(s.Must, json.RawMessage(fmt.Sprintf(q, key, val)))
	}
}

func (s *pageSearchFilter) transformFields() {
	fields := s.Fields

	if len(fields) == 0 {
		fields = pageDefaultFields
	}

	s.FieldConstraint = util.ToEscapedStringList(fields)
}

func (s *pageSearchFilter) transformDateRange() {
	daysLookback := s.Range

	if daysLookback == 0 {
		daysLookback = pageDefaultDaysLookback
	}
	lookback := time.Now().AddDate(0, 0, -daysLookback).Unix()
	part := json.RawMessage(fmt.Sprintf(dateRangeTemplate, lookback))

	s.Must = append(s.Must, part)
}

func (s *pageSearchFilter) buildQuery() (io.Reader, error) {
	b, err := json.Marshal(s.Must)

	if err != nil {
		return nil, errors.Wrap(err, "failed to serialize json when building article search query")
	}

	query := fmt.Sprintf(queryTemplate, s.FieldConstraint, string(b))

	return strings.NewReader(query), nil

}
