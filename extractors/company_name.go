package extractors

import (
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

func (s *companyNameExtractor) Perform(f *os.File, meta message.FetcherResponse, composite types.CompositeAnalysis) (interface{}, error) {
	var results []string

	for _, company := range s.companies {
		if c := company.Regex.Find(composite.TextContent); c != nil {
			results = append(results, company.Identifier)
		}
	}

	return features.Corporations(util.DedupeStrSlice(results)), nil
}

func (s *companyNameExtractor) Name() string {
	return types.CompanyNameExtractor
}

func (s *companyNameExtractor) Requires() []string {
	return []string{
		types.TextExtractor,
	}
}
