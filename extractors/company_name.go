package extractors

import (
	"fmt"
	"os"

	log "github.com/sirupsen/logrus"

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
