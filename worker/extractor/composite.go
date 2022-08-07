package extractor

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/extractors"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/resource/objectstore"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/worker"
	"github.com/pkg/errors"
)

type compositeResult struct {
	Extractor extractors.Extractor
	Result    interface{}
}

type compositeExtractor struct {
	Enabled          []string
	ObjectStore      objectstore.ObjectStore
	TransformerQueue queue.Queue
}

type CompositeArgs struct {
	Enabled          []string                `json:"enabled"`
	ObjectStore      objectstore.ObjectStore `json:"-" resource:"object_store"`
	TransformerQueue queue.Queue             `json:"-" resource:"transformer_queue"`
}

func NewCompositeExtractorWorker(opts CompositeArgs) worker.Worker {
	return &compositeExtractor{
		Enabled:          opts.Enabled,
		ObjectStore:      opts.ObjectStore,
		TransformerQueue: opts.TransformerQueue,
	}
}

func (s *compositeExtractor) executeExtractors(path string, meta message.FetcherResponse) (*message.CompositeAnalysis, error) {
	composite := &message.CompositeAnalysis{
		FetcherResponse: meta,
	}
	pending := s.getExtractors()
	var completed []string
	var errs []error

	log.Printf("executing %d extractors for uri %s", len(pending), meta.URI)

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

	log.Printf("executed %d extractors from uri %s", len(completed), meta.URI)

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
		log.Errorf("failed to open file for extractor %s %s: %s", ext.Name(), path, err)
	}

	result, err := ext.Perform(f, composite)

	if err != nil {
		log.Errorf("failed to execute extractor %s: %s", ext.Name(), err)
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

	f, err := s.ObjectStore.Get(meta.StoreKey)

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

	if objectStoreErr := s.ObjectStore.Delete(meta.StoreKey); err != nil {
		log.Errorf("failed to delete object in store after extraction: %s", objectStoreErr)
	}

	if exists, err := util.PathExists(path); exists {
		if delErr := os.Remove(path); err != nil {
			log.Errorf("failed to delete file after extraction: %s", delErr)
		}
	} else if err != nil {
		log.Errorf("failed to stat file for deletion: %s", err)
	}

	if transformerErr := s.sendToTransformerQueue(composite); transformerErr != nil {
		log.Errorf(transformerErr.Error())
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
	case message.SentimentExtractor:
		return extractors.NewSentimentExtractor()
	case message.TextExtractor:
		return extractors.NewTextExtractor()
	case message.NgramExtractor:
		return extractors.NewNgramExtractor()
	case message.TitleExtractor:
		return extractors.NewTitleExtractor()
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

func (s *compositeExtractor) sendToTransformerQueue(composite *message.CompositeAnalysis) error {
	b, err := json.Marshal(composite)

	if err != nil {
		return errors.Wrap(err, "composite failed to serialize transformer message")
	}

	transformerMsg := types.Message{
		ID:          string(composite.RequestID),
		MessageType: types.CompositeAnalysisType,
		Message:     json.RawMessage(b),
	}

	return errors.Wrap(
		s.TransformerQueue.Put(transformerMsg, 0),
		"composite failed to send transformer message",
	)

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
		log.Errorf("errors during extraction:\n%s", errStr)
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
