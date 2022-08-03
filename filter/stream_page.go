package filter

import (
	"encoding/json"
	"time"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/rpc"
	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
)

type pageStreamFilter struct {
	rpc.FilterParams
	rpc.PageFilterQuery
}

func NewPageStreamFilter(params rpc.FilterParams) StreamFilter {
	pageFilter := params.Query.(rpc.PageFilterQuery)

	return &pageStreamFilter{
		FilterParams:    params,
		PageFilterQuery: pageFilter,
	}
}

func (s *pageStreamFilter) Perform(entities []*types.Indexable) (results []json.RawMessage, err error) {
	for _, entity := range entities {
		page := entity.Data.(types.Page)
		if s.filterPage(page) {
			if b, err := json.Marshal(page); err != nil {
				return nil, errors.Wrap(err, "failed to serialize page json during filter")
			} else {
				results = append(results, b)
			}
		}

	}

	return
}

func (s *pageStreamFilter) filterPage(page types.Page) bool {
	if !s.filterString(page.Uri, s.Url) {
		return false
	}
	if !s.filterString(page.Origin, s.Domain) {
		return false
	}
	if !s.filterString(page.Language, s.Language) {
		return false
	}
	if !s.filterString(page.Title, s.Title) {
		return false
	}
	if !s.filterInt(page.HttpCode, s.HttpCode) {
		return false
	}
	if !s.filterFields(page) {
		return false
	}
	if !s.filterDateRange(page) {
		return false
	}

	return true
}

func (s *pageStreamFilter) filterString(v string, vals []string) bool {
	if len(vals) == 0 {
		return true
	}

	for _, val := range vals {
		if v == val {
			return true
		}
	}

	return false
}

func (s *pageStreamFilter) filterInt(v int, vals []int) bool {
	if len(vals) == 0 {
		return true
	}

	for _, val := range vals {
		if v == val {
			return true
		}
	}

	return false
}

func (s *pageStreamFilter) filterFields(page types.Page) bool {
	fields := s.Fields

	if len(fields) == 0 {
		fields = articleDefaultFields
	}

	for _, field := range fields {
		if util.IsNullByCheckingStructTag(page, field) {
			return false
		}
	}

	return true
}

func (s *pageStreamFilter) filterDateRange(page types.Page) bool {
	daysLookback := s.Range

	if daysLookback == 0 {
		daysLookback = articleDefaultDaysLookback
	}
	lookback := time.Now().AddDate(0, 0, -daysLookback).Unix()

	return page.Timestamp >= lookback
}
