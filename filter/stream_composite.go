package filter

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/types/rpc"
	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type compositeStreamFilter struct {
	rpc.FilterParams
	rpc.CompositeFilterQuery
}

func NewCompositeStreamFilter(params rpc.FilterParams) StreamFilter {
	compositeFilter := params.Query.(rpc.CompositeFilterQuery)

	return &compositeStreamFilter{
		FilterParams:         params,
		CompositeFilterQuery: compositeFilter,
	}
}

func (s *compositeStreamFilter) Perform(entities []*types.Indexable) (results []json.RawMessage, err error) {
	for _, entity := range entities {
		composite := entity.Data.(message.CompositeAnalysis)

		if s.filterComposite(composite) {
			if b, err := projectComposite(composite, s.Fields); err != nil {
				return nil, errors.Wrap(err, "failed to serialize composite json during filter")
			} else {
				results = append(results, b)
			}
		}
	}

	return
}

func (s *compositeStreamFilter) filterComposite(composite message.CompositeAnalysis) bool {
	for _, filter := range s.getFilters() {
		if !filter(composite) {
			return false
		}
	}

	return true
}

func (s *compositeStreamFilter) getFilters() []func(composite message.CompositeAnalysis) bool {
	return []func(composite message.CompositeAnalysis) bool{
		s.filterDateRange,
		s.filterCountry,
		s.filterKeyword,
		s.filterCompany,
		s.filterFields,
		s.filterUrl,
		s.filterDomain,
		s.filterHttpCode,
		s.filterTitle,
		s.filterLanguage,
	}
}

func (s *compositeStreamFilter) filterDateRange(composite message.CompositeAnalysis) bool {
	daysLookback := s.Range

	if daysLookback == 0 {
		daysLookback = compositeDefaultDaysLookback
	}
	lookback := time.Now().AddDate(0, 0, -daysLookback).Unix()

	return composite.Timestamp >= lookback
}

func (s *compositeStreamFilter) filterCountry(composite message.CompositeAnalysis) bool {
	if len(s.Country) == 0 {
		return true
	}

	var countries []string

	if err := composite.Load(features.CountryField, &countries); err != nil {
		log.Warnf("composite has invalid country field: %s", composite.RequestID)
		return false
	}

	matches := 0

	for _, country := range s.Country {
		if util.StringInSlice(country, countries) {
			matches += 1
		}
	}

	return matches == len(s.Country)
}

func (s *compositeStreamFilter) filterKeyword(composite message.CompositeAnalysis) bool {
	fields := []string{
		features.TitleField,
		features.TextField,
	}

	matches := 0

	for _, substr := range s.Keyword {
		var str string

		for _, field := range fields {
			if err := composite.Load(field, &str); err != nil {
				log.Warnf("composite has invalid %s field: %s", field, composite.RequestID)
				continue
			}

			if strings.Contains(strings.ToLower(str), strings.ToLower(substr)) {
				matches += 1
				break
			}
		}
	}

	return matches == len(s.Keyword)
}

func (s *compositeStreamFilter) filterCompany(composite message.CompositeAnalysis) bool {
	if len(s.Company) == 0 {
		return true
	}

	var companies []string

	if err := composite.Load(features.CompanyNameField, &companies); err != nil {
		log.Warnf("composite has invalid company field: %s", composite.RequestID)
		return false
	}

	matches := 0

	for _, company := range s.Company {
		if util.StringInSlice(company, companies) {
			matches += 1
		}
	}

	return matches == len(s.Company)

}

func (s *compositeStreamFilter) filterFields(composite message.CompositeAnalysis) bool { return true }

func (s *compositeStreamFilter) filterUrl(composite message.CompositeAnalysis) bool {
	return filterString(composite.URI, s.Url)
}

func (s *compositeStreamFilter) filterDomain(composite message.CompositeAnalysis) bool {
	return filterString(composite.Host, s.Domain)
}

func (s *compositeStreamFilter) filterHttpCode(composite message.CompositeAnalysis) bool {
	return filterInt(composite.HTTPCode, s.HttpCode)
}

func (s *compositeStreamFilter) filterTitle(composite message.CompositeAnalysis) bool {
	return filterStringField(composite, features.TitleField, s.Title)
}

func (s *compositeStreamFilter) filterLanguage(composite message.CompositeAnalysis) bool {
	if len(s.Language) == 0 {
		return true
	}
	lang := features.Language{}

	if err := composite.Load(features.LanguageField, &lang); err != nil {
		log.Warnf("composite has invalid language field: %s", composite.RequestID)
		return false
	}

	return filterString(lang.Name, s.Language)
}

func filterStringField(composite message.CompositeAnalysis, field string, query []string) bool {
	if len(query) == 0 {
		return true
	}
	var str string

	if err := composite.Load(field, &str); err != nil {
		log.Warnf("composite has invalid %s field: %s", field, composite.RequestID)
		return false
	}

	return filterString(str, query)

}

func filterString(v string, vals []string) bool {
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

func filterInt(v int, vals []int) bool {
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

func projectComposite(composite message.CompositeAnalysis, fields []string) ([]byte, error) {
	if len(fields) == 0 {
		fields = compositeDefaultFields
	}
	data, err := json.Marshal(composite)
	if err != nil {
		return nil, err
	}
	var source map[string]interface{}
	if err = json.Unmarshal(data, &source); err != nil {
		return nil, err
	}
	result := map[string]interface{}{}
	for _, field := range fields {
		parts := strings.Split(field, ".")
		var value interface{} = source
		found := true
		for _, part := range parts {
			object, ok := value.(map[string]interface{})
			if !ok {
				found = false
				break
			}
			value, ok = object[part]
			if !ok {
				found = false
				break
			}
		}
		if !found {
			continue
		}
		target := result
		for _, part := range parts[:len(parts)-1] {
			child, ok := target[part].(map[string]interface{})
			if !ok {
				child = map[string]interface{}{}
				target[part] = child
			}
			target = child
		}
		target[parts[len(parts)-1]] = value
	}
	return json.Marshal(result)
}
