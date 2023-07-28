package extractors

import (
	"os"

	"github.com/pkg/errors"
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
	var textContent string

	if err := composite.Load(features.TextField, &textContent); err != nil {
		return nil, errors.Wrap(err, "company name extractor")
	}

	for _, company := range s.companies {
		if c := company.Regex.Find([]byte(textContent)); c != nil {
			results = append(results, company.Identifier)
		}
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
