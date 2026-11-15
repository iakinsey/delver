package extractors

import (
	"context"
	"os"
	"strings"

	"github.com/pkg/errors"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
)

type companyNameExtractor struct {
	companies []*types.Company
}

func NewCompanyNameExtractor() Extractor {
	conf := config.Get()
	companies, err := types.GetCompanies(conf.CompaniesPath)

	if err != nil {
		panic(err)
	}

	return &companyNameExtractor{
		companies: companies,
	}
}

func (s *companyNameExtractor) Perform(f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	return s.PerformContext(context.Background(), f, composite)
}
func (s *companyNameExtractor) PerformContext(ctx context.Context, f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var results []string
	var textContent string

	if err := composite.Load(features.TextField, &textContent); err != nil {
		return nil, errors.Wrap(err, "company name extractor")
	}

	for _, company := range s.companies {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if c := company.Regex.FindReaderIndex(contextRuneReader{ctx, strings.NewReader(textContent)}); c != nil {
			results = append(results, company.Identifier)
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return features.Corporations(util.DedupeStrSlice(results)), nil
}

func (s *companyNameExtractor) Name() string {
	return features.CompanyNameField
}

func (s *companyNameExtractor) Requires() []string {
	return []string{
		features.TextField,
	}
}
