package extractor

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"reflect"
	"strings"

	"github.com/iakinsey/delver/extractors"
	"github.com/iakinsey/delver/gateway/streamstore"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/worker"
	"github.com/pkg/errors"
)

type compositeExtractor struct {
	opts        CompositeArgs
	StreamStore streamstore.StreamStore
}

type CompositeArgs struct {
	StreamStore streamstore.StreamStore
}

func NewCompositeExtractorWorker(opts CompositeArgs) worker.Worker {
	return &compositeExtractor{
		opts:        opts,
		StreamStore: opts.StreamStore,
	}
}

func (s *compositeExtractor) executeExtractors(path string, meta message.FetcherResponse) (*types.CompositeAnalysis, error) {
	// Start by executing every nondependent extractor
	// When an extractor completes add its completion state to a map[string]bool
	// Check every remaining exeutor that hasnt run yet, if its dependencies indicate completion in the map, then run
	// If no new executors were queued, and none are pending, return

	composite := &types.CompositeAnalysis{}
	pending := s.getExtractors()
	var completed []string

	for len(pending) > 0 {
		var toExecute []extractors.Extractor

		for _, ext := range pending {
			if s.canExecuteExtractor(ext, completed) {
				toExecute = append(toExecute, ext)
			}
		}

		if len(toExecute) == 0 && len(completed) == 0 {
			return nil, errors.New("failed to find extractors to execute")
		} else if len(toExecute) == 0 {
			return composite, nil
		}

		// TODO accumulate errors when running executeExtractorSet
		s.executeExtractorSet(toExecute, path, meta, composite)
	}

	return composite, nil
}

func (s *compositeExtractor) canExecuteExtractor(ext extractors.Extractor, completed []string) bool {
	requires := ext.Requires()

	if len(requires) == 0 {
		return true
	}

	for _, requirement := range requires {
		if !util.StringInSlice(requirement, completed) {
			return false
		}
	}

	return true
}

func (s *compositeExtractor) executeExtractorSet(exts []extractors.Extractor, path string, meta message.FetcherResponse, composite *types.CompositeAnalysis) {
	results := make(chan interface{}, len(exts))

	for _, ext := range exts {
		go s.executeExtractor(ext, path, meta, *composite, results)
	}

	// TODO add timeouts to execution
	for i := 0; i < len(exts); i++ {
		types.UpdateCompositeAnalysis(<-results, composite)
	}
}

func (s *compositeExtractor) executeExtractor(ext extractors.Extractor, path string, meta message.FetcherResponse, composite types.CompositeAnalysis, results chan interface{}) {
	f, err := os.Open(path)

	if err != nil {
		log.Printf("failed to open file for extractor %s %s: %s", ext.Name(), path, err)
	}

	result, err := ext.Perform(f, meta, composite)

	if err != nil {
		log.Printf("failed to execute extractor %s: %s", ext.Name(), err)
	}

	results <- result
}

func (s *compositeExtractor) OnMessage(msg types.Message) (interface{}, error) {
	meta := message.FetcherResponse{}

	if err := json.Unmarshal(msg.Message, &meta); err != nil {
		return nil, err
	}

	f, err := s.StreamStore.Get(meta.StoreKey)

	if err != nil {
		return nil, err
	}

	composite, err := s.executeExtractors(f.Name(), meta)

	if err != nil {
		return nil, err
	}

	if streamStoreErr := s.StreamStore.Delete(meta.StoreKey); err != nil {
		log.Printf("failed to delete stream store object after extraction: %s", streamStoreErr)
	}

	return *composite, nil
}

func (s *compositeExtractor) OnComplete() {}

func (s *compositeExtractor) getExtractor(name string) extractors.Extractor {
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

func (s *compositeExtractor) getExtractors() (result []extractors.Extractor) {
	for _, name := range types.ExtractorNames {
		result = append(result, s.getExtractor(name))
	}

	return
}

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
