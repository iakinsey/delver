package filter

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/rpc"
	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
)

type articleStreamFilter struct {
	rpc.FilterParams
	rpc.ArticleFilterQuery
}

func NewArticleStreamFilter(params rpc.FilterParams) StreamFilter {
	articleFilter := params.Query.(rpc.ArticleFilterQuery)

	return &articleStreamFilter{
		FilterParams:       params,
		ArticleFilterQuery: articleFilter,
	}
}

func (s *articleStreamFilter) Perform(entities []*types.Indexable) (results []json.RawMessage, err error) {
	for _, entity := range entities {
		article := entity.Data.(types.Article)

		if s.filterArticle(article) {
			if b, err := json.Marshal(article); err != nil {
				return nil, errors.Wrap(err, "failed to serialize article json during filter")
			} else {
				results = append(results, b)
			}
		}
	}

	return
}

func (s *articleStreamFilter) filterArticle(article types.Article) bool {
	for _, filter := range s.getFilters() {
		if !filter(article) {
			return false
		}
	}

	return true
}

func (s *articleStreamFilter) getFilters() []func(article types.Article) bool {
	return []func(article types.Article) bool{
		s.filterDateRange,
		s.filterCountry,
		s.filterKeyword,
		s.filterCompany,
		s.filterFields,
	}
}

func (s *articleStreamFilter) filterDateRange(article types.Article) bool {
	daysLookback := s.Range

	if daysLookback == 0 {
		daysLookback = articleDefaultDaysLookback
	}
	lookback := time.Now().AddDate(0, 0, -daysLookback).Unix()

	return article.Found >= lookback
}

func (s *articleStreamFilter) filterCountry(article types.Article) bool {
	if len(s.Country) == 0 {
		return true
	}

	for _, country := range s.Country {
		if !util.StringInSlice(country, article.Countries) {
			return false
		}
	}

	return true
}

func (s *articleStreamFilter) filterKeyword(article types.Article) bool {
	fields := []string{
		article.Title,
		article.Summary,
		article.Content,
	}

	for _, substr := range s.Fields {
		found := false

		for _, field := range fields {
			if strings.Contains(field, substr) {
				found = true
				break
			}
		}

		if !found {
			return false
		}
	}

	return true
}

func (s *articleStreamFilter) filterCompany(article types.Article) bool {
	if len(s.Company) == 0 {
		return true
	}

	for _, company := range s.Company {
		if !util.StringInSlice(company, article.Corporate) {
			return false
		}
	}

	return true
}

func (s *articleStreamFilter) filterFields(article types.Article) bool {
	fields := s.Fields

	if len(fields) == 0 {
		fields = articleDefaultFields
	}

	for _, field := range fields {
		if util.IsNullByCheckingStructTag(article, field) {
			return false
		}
	}

	return true
}
