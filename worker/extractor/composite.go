package extractor

import (
	"github.com/iakinsey/delver/extractors"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/worker"
)

type compositeExtractor struct {
}

func NewCompositeExtractorWorker() worker.Worker {
	return &compositeExtractor{}
}

func (s *compositeExtractor) OnMessage(msg types.Message) (interface{}, error) {
	return nil, nil
}

func (s *compositeExtractor) OnComplete() {}

func (s *compositeExtractor) getExtractor(name string) extractors.Extractor {
	switch name {
	case types.UrlExtractor:
		return extractors.NewUrlExtractor()
	case types.AdversarialExtractor:
	case types.BlacklistedExtractor:
	case types.CompanyNameExtractor:
	case types.CountryExtractor:
	case types.LanguageExtractor:
	case types.NgramExtractor:
	case types.SentimentExtractor:
	case types.TextExtractor:
	default:
		return nil
	}
}
