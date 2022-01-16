package extractor

import (
	"encoding/json"
	"errors"
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
)

type compositeResult struct {
	Extractor extractors.Extractor
	Result    interface{}
}

type compositeExtractor struct {
	Enabled     []string
	StreamStore streamstore.StreamStore
}

type CompositeArgs struct {
	Enabled     []string
	StreamStore streamstore.StreamStore
}

func NewCompositeExtractorWorker(opts CompositeArgs) worker.Worker {
	return &compositeExtractor{
		Enabled:     opts.Enabled,
		StreamStore: opts.StreamStore,
	}
}

func (s *compositeExtractor) executeExtractors(path string, meta message.FetcherResponse) (*message.CompositeAnalysis, error) {
	composite := &message.CompositeAnalysis{
		FetcherResponse: meta,
	}
	pending := s.getExtractors()
	var completed []string
	var errs []error

	for len(pending) > 0 {
		var toExecute []extractors.Extractor

		for _, ext := range pending {
			if s.canExecuteExtractor(ext, completed) {
				toExecute = append(toExecute, ext)
			}
		}

		if len(toExecute) == 0 && len(completed) == 0 {
			errs := append(errs, errors.New("failed to find extractors to execute"))
			return nil, getCompositeError(composite, errs)
		} else if len(toExecute) == 0 {
			return composite, getCompositeError(composite, errs)
		}

		newCompleted, newErrs := s.executeExtractorSet(toExecute, path, composite)
		completed = append(completed, newCompleted...)
		errs = append(errs, newErrs...)
		pending = s.getNextPending(pending, toExecute)
	}

	return composite, getCompositeError(composite, errs)
}

func (s *compositeExtractor) getNextPending(pending []extractors.Extractor, completed []extractors.Extractor) (next []extractors.Extractor) {
	for _, ext := range pending {
		if !ExtractorInSlice(ext, completed) {
			next = append(next, ext)
		}
	}

	return
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

func (s *compositeExtractor) executeExtractorSet(exts []extractors.Extractor, path string, composite *message.CompositeAnalysis) ([]string, []error) {
	var errors []error
	var completed []string
	results := make(chan compositeResult, len(exts))

	for _, ext := range exts {
		go s.executeExtractor(ext, path, *composite, results)
	}

	// TODO add timeouts to execution
	for i := 0; i < len(exts); i++ {
		if newComplete, err := s.updateCompositeAnalysis(<-results, composite); err != nil {
			errors = append(errors, err)
		} else {
			completed = append(completed, newComplete)
		}
	}

	return completed, errors
}

func (s *compositeExtractor) updateCompositeAnalysis(result compositeResult, composite *message.CompositeAnalysis) (string, error) {
	name := result.Extractor.Name()

	switch d := result.Result.(type) {
	case error:
		return name, d
	case nil:
		return name, nil
	default:
		return name, result.Extractor.SetResult(result.Result, composite)
	}
}

func (s *compositeExtractor) executeExtractor(ext extractors.Extractor, path string, composite message.CompositeAnalysis, results chan compositeResult) {
	f, err := os.Open(path)

	if err != nil {
		log.Printf("failed to open file for extractor %s %s: %s", ext.Name(), path, err)
	}

	result, err := ext.Perform(f, composite)

	if err != nil {
		log.Printf("failed to execute extractor %s: %s", ext.Name(), err)
	}

	results <- compositeResult{
		Extractor: ext,
		Result:    result,
	}
}

func (s *compositeExtractor) OnMessage(msg types.Message) (interface{}, error) {
	meta := message.FetcherResponse{}
	var result interface{} = nil

	if err := json.Unmarshal(msg.Message, &meta); err != nil {
		return nil, err
	}

	f, err := s.StreamStore.Get(meta.StoreKey)

	if err != nil {
		return nil, err
	}

	path := f.Name()

	if err = f.Close(); err != nil {
		return nil, err
	}

	composite, err := s.executeExtractors(path, meta)

	if composite != nil {
		result = *composite
	}

	if streamStoreErr := s.StreamStore.Delete(meta.StoreKey); err != nil {
		log.Printf("failed to delete stream store object after extraction: %s", streamStoreErr)
	}

	if delErr := os.Remove(f.Name()); !os.IsNotExist(delErr) {
		log.Printf("failed to delete file after extraction: %s", delErr)
	}

	return result, err
}

func (s *compositeExtractor) OnComplete() {}

func (s *compositeExtractor) getExtractor(name string) extractors.Extractor {
	switch name {
	case message.UrlExtractor:
		return extractors.NewUrlExtractor()
	case message.AdversarialExtractor:
		return extractors.NewAdversarialExtractor()
	case message.CompanyNameExtractor:
		return extractors.NewCompanyNameExtractor()
	case message.CountryExtractor:
		return extractors.NewCountryExtractor()
	case message.LanguageExtractor:
		return extractors.NewLanguageExtractor()
	case message.NgramExtractor:
		return extractors.NewNgramExtractor()
	case message.SentimentExtractor:
		return extractors.NewSentimentExtractor()
	case message.TextExtractor:
		return extractors.NewTextExtractor()
	default:
		return nil
	}
}

func (s *compositeExtractor) getExtractors() (result []extractors.Extractor) {
	for _, name := range s.Enabled {
		result = append(result, s.getExtractor(name))
	}

	return
}

func getCompositeError(composite *message.CompositeAnalysis, errors []error) error {
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

func ExtractorInSlice(a extractors.Extractor, l []extractors.Extractor) bool {
	for _, b := range l {
		if a.Name() == b.Name() {
			return true
		}
	}

	return false
}
