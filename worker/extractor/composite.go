package extractor

import (
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"strings"

	"github.com/iakinsey/delver/extractors"
	"github.com/iakinsey/delver/gateway/streamstore"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/worker"
)

type compositeExtractor struct {
	extractors  []extractors.Extractor
	StreamStore streamstore.StreamStore
}

type CompositeArgs struct {
	Extractors  []string
	StreamStore streamstore.StreamStore
}

func NewCompositeExtractorWorker(opts CompositeArgs) worker.Worker {
	var exts []extractors.Extractor

	for _, name := range opts.Extractors {
		if e := getExtractor(name, opts); e != nil {
			exts = append(exts, e)
		} else {
			log.Fatalf("unknown extractor name: %s", name)
		}
	}

	exts = sortExtractorDeps(exts)

	return &compositeExtractor{
		extractors:  exts,
		StreamStore: opts.StreamStore,
	}
}

func sortExtractorDeps(exts []extractors.Extractor) []extractors.Extractor {
	// TODO Sort extractors based on their required dependencies
	return exts
}

func (s *compositeExtractor) OnMessage(msg types.Message) (interface{}, error) {
	var errors []error
	composite := types.CompositeAnalysis{}
	meta := message.FetcherResponse{}

	if err := json.Unmarshal(msg.Message, &meta); err != nil {
		return nil, err
	}

	f, err := s.StreamStore.Get(meta.StoreKey)

	if err != nil {
		return nil, err
	}

	for _, extractor := range s.extractors {
		data, err := extractor.Perform(f, meta, composite)

		if err != nil {
			errors = append(errors, err)
		}

		if data != nil {
			types.UpdateCompositeAnalysis(data, &composite)
		}
	}

	if err := s.StreamStore.Delete(meta.StoreKey); err != nil {
		errors = append(errors, err)
	}

	return composite, getCompositeError(&composite, errors)
}

func (s *compositeExtractor) OnComplete() {}

func getCompositeError(composite *types.CompositeAnalysis, errors []error) error {
	noAnalysis := reflect.DeepEqual(composite, nil)
	hasErrors := len(errors) != 0
	errStr := ""

	if hasErrors {
		var errStrs []string

		for _, err := range errors {
			errStrs = append(errStrs, err.Error())
		}

		errStr = strings.Join(errStrs, "\n")
	}

	if noAnalysis && hasErrors {
		return fmt.Errorf("fatal error during extraction\n%s", errStr)
	} else if hasErrors {
		log.Printf("errors during extraction:\n%s", errStr)
	}

	return nil
}

func getExtractor(name string, args CompositeArgs) extractors.Extractor {
	// TODO have a constant list of extractor instantiation methods and use a map here instead
	// based on its Name() values
	switch name {
	case types.UrlExtractor:
		return extractors.NewUrlExtractor()
	case types.AdversarialExtractor:
		return extractors.NewAdversarialExtractor()
	case types.CompanyNameExtractor:
		return extractors.NewCompanyNameExtractor()
	case types.CountryExtractor:
		return extractors.NewCountryExtractor()
	case types.LanguageExtractor:
		return extractors.NewLanguageExtractor()
	case types.NgramExtractor:
		return extractors.NewNgramExtractor()
	case types.SentimentExtractor:
		return extractors.NewSentimentExtractor()
	case types.TextExtractor:
		return extractors.NewTextExtractor()
	default:
		return nil
	}
}
