package extractors

import (
	"fmt"
	"log"
	"os"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
)

const companiesFileName = "companies.json"

type companyNameExtractor struct {
	companies []*types.Company
}

func NewCompanyNameExtractor() Extractor {
	companies, err := types.GetCompanies(util.DataFilePath(companiesFileName))

	if err != nil {
		log.Fatalf(err.Error())
	}

	return &companyNameExtractor{
		companies: companies,
	}
}

func (s *companyNameExtractor) Perform(f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	var results []string

	for _, company := range s.companies {
		if c := company.Regex.Find([]byte(composite.TextContent)); c != nil {
			results = append(results, company.Identifier)
		}
	}

	return features.Corporations(util.DedupeStrSlice(results)), nil
}

func (s *companyNameExtractor) Name() string {
	return message.CompanyNameExtractor
}

func (s *companyNameExtractor) Requires() []string {
	return []string{
		message.TextExtractor,
	}
}

func (s *companyNameExtractor) SetResult(result interface{}, composite *message.CompositeAnalysis) error {
	switch d := result.(type) {
	case features.Corporations:
		composite.Corporations = d
		return nil
	default:
		return fmt.Errorf("CompanyNameExtractor: attempt to cast unknown type")
	}
}
