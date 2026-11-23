package extractor

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/extractors"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/resource/objectstore"
	"github.com/iakinsey/delver/transformers"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
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
	configuration    *config.Config
	active           sync.WaitGroup
	Timeout          time.Duration
	initOnce         sync.Once
	cached           []extractors.Extractor
	initErr          error
	Enabled          []string
	ObjectStore      objectstore.ObjectStore
	TransformerQueue queue.Queue
}

type CompositeArgs struct {
	Timeout          time.Duration           `json:"timeout"`
	Enabled          []string                `json:"enabled"`
	ObjectStore      objectstore.ObjectStore `json:"-" resource:"object_store"`
	TransformerQueue queue.Queue             `json:"-" resource:"transformer_queue"`
}

func NewCompositeExtractorWorker(opts CompositeArgs) worker.Worker {
	return NewCompositeExtractorWorkerWithConfig(opts, config.Get())
}
func NewCompositeExtractorWorkerWithConfig(opts CompositeArgs, c config.Config) worker.Worker {
	s := &compositeExtractor{
		configuration:    &c,
		Enabled:          opts.Enabled,
		Timeout:          opts.Timeout,
		ObjectStore:      opts.ObjectStore,
		TransformerQueue: opts.TransformerQueue,
	}
	if s.Timeout == 0 {
		s.Timeout = 30 * time.Second
	}
	s.getExtractors()
	if s.initErr != nil {
		panic(s.initErr)
	}
	return s
}

func (s *compositeExtractor) executeExtractors(path string, meta message.FetcherResponse) (*message.CompositeAnalysis, error) {
	return s.executeExtractorsContext(context.Background(), path, meta)
}
func (s *compositeExtractor) executeExtractorsContext(ctx context.Context, path string, meta message.FetcherResponse) (*message.CompositeAnalysis, error) {
	composite := &message.CompositeAnalysis{
		FetcherResponse: meta,
		Features:        make(map[string]interface{}),
	}
	pending := s.getExtractors()
	ctx = extractors.WithPreparedAnalysisFor(ctx, pending)
	if s.initErr != nil {
		return nil, s.initErr
	}
	var completed []string
	var errs []error

	log.Printf("executing %d extractors for uri %s", len(pending), meta.URI)

	for len(pending) > 0 {
		var toExecute []extractors.Extractor

		for _, ext := range pending {
			if s.canExecuteExtractor(ext, completed, composite) {
				toExecute = append(toExecute, ext)
			}
		}

		if len(toExecute) == 0 && len(completed) == 0 {
			errs := append(errs, errors.New("failed to find extractors to execute"))
			return nil, getCompositeError(composite, errs)
		} else if len(toExecute) == 0 {
			return composite, errors.New("extractor dependencies were not produced")
		}

		newCompleted, newErrs := s.executeExtractorSetContext(ctx, toExecute, path, composite)
		for _, err := range newErrs {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return nil, err
			}
		}
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

func (s *compositeExtractor) canExecuteExtractor(ext extractors.Extractor, completed []string, composite *message.CompositeAnalysis) bool {
	requires := ext.Requires()

	if len(requires) == 0 {
		return true
	}

	for _, requirement := range requires {
		if !util.StringInSlice(requirement, completed) || !composite.Has(requirement) {
			return false
		}
	}

	return true
}

func (s *compositeExtractor) executeExtractorSet(exts []extractors.Extractor, path string, composite *message.CompositeAnalysis) ([]string, []error) {
	return s.executeExtractorSetContext(context.Background(), exts, path, composite)
}
func (s *compositeExtractor) executeExtractorSetContext(ctx context.Context, exts []extractors.Extractor, path string, composite *message.CompositeAnalysis) ([]string, []error) {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var errors []error
	var completed []string
	results := make(chan compositeResult, len(exts))

	for _, ext := range exts {
		snapshot := *composite
		snapshot.ExtractorErrors = make(map[string]string, len(composite.ExtractorErrors))
		for key, value := range composite.ExtractorErrors {
			snapshot.ExtractorErrors[key] = value
		}
		snapshot.Features = make(map[string]interface{}, len(composite.Features))
		for key, value := range composite.Features {
			snapshot.Features[key] = value
		}
		s.active.Add(1)
		go func(ext extractors.Extractor, snapshot message.CompositeAnalysis) {
			defer s.active.Done()
			s.executeExtractorContext(ctx, ext, path, snapshot, results)
		}(ext, snapshot)
	}

	for i := 0; i < len(exts); i++ {
		var result compositeResult
		select {
		case result = <-results:
		case <-ctx.Done():
			return completed, append(errors, ctx.Err())
		}
		if newComplete, err := s.updateCompositeAnalysis(result, composite); err != nil {
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
		if composite.ExtractorErrors == nil {
			composite.ExtractorErrors = map[string]string{}
		}
		composite.ExtractorErrors[name] = d.Error()
		return name, d
	case nil:
		return name, nil
	default:
		composite.Features[name] = result.Result
		return name, nil
	}
}

func (s *compositeExtractor) executeExtractor(ext extractors.Extractor, path string, composite message.CompositeAnalysis, results chan compositeResult) {
	s.executeExtractorContext(context.Background(), ext, path, composite, results)
}
func (s *compositeExtractor) executeExtractorContext(ctx context.Context, ext extractors.Extractor, path string, composite message.CompositeAnalysis, results chan compositeResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			results <- compositeResult{Extractor: ext, Result: fmt.Errorf("extractor panic: %v", recovered)}
		}
	}()
	f, err := os.Open(path)

	if err != nil {
		results <- compositeResult{Extractor: ext, Result: err}
		return
	}

	defer f.Close()
	var result interface{}
	if contextual, ok := ext.(extractors.ContextExtractor); ok {
		result, err = contextual.PerformContext(ctx, f, composite)
	} else {
		result, err = ext.Perform(f, composite)
	}

	if err != nil {
		result = err
	}

	results <- compositeResult{
		Extractor: ext,
		Result:    result,
	}
}

func (s *compositeExtractor) OnMessage(msg types.Message) (interface{}, error) {
	return s.OnMessageContext(context.Background(), msg)
}
func (s *compositeExtractor) OnMessageContext(ctx context.Context, msg types.Message) (interface{}, error) {
	meta := message.FetcherResponse{}
	var result interface{} = nil

	if err := json.Unmarshal(msg.Message, &meta); err != nil {
		return nil, err
	}

	if !meta.Success && meta.StoreKey == "" {
		failed := message.CompositeAnalysis{FetcherResponse: meta, Features: map[string]interface{}{}}
		if err := transformers.SendContext(ctx, s.TransformerQueue, string(meta.RequestID), types.CompositeAnalysisType, failed); err != nil {
			return nil, err
		}
		return failed, nil
	}
	f, err := s.ObjectStore.Get(meta.StoreKey)

	if err != nil {
		return nil, err
	}

	path := f.Name()

	if err = f.Close(); err != nil {
		return nil, err
	}

	composite, extractorErr := s.executeExtractorsContext(ctx, path, meta)

	if composite == nil {
		return nil, extractorErr
	}
	result = *composite

	transformerErr := transformers.SendContext(ctx,
		s.TransformerQueue,
		string(composite.RequestID),
		types.CompositeAnalysisType,
		composite,
	)

	if transformerErr != nil {
		return nil, transformerErr
	}

	return result, extractorErr
}

func (s *compositeExtractor) OnComplete() { s.active.Wait() }

func (s *compositeExtractor) getExtractor(name string) extractors.Extractor {

	switch name {
	case features.UrlField:
		return extractors.NewUrlExtractor()
	case features.AdversarialField:
		if s.configuration != nil {
			return extractors.NewAdversarialExtractorWithConfig(*s.configuration)
		}
		return extractors.NewAdversarialExtractor()
	case features.CompanyNameField:
		if s.configuration != nil {
			return extractors.NewCompanyNameExtractorWithConfig(*s.configuration)
		}
		return extractors.NewCompanyNameExtractor()
	case features.CountryField:
		if s.configuration != nil {
			return extractors.NewCountryExtractorWithConfig(*s.configuration)
		}
		return extractors.NewCountryExtractor()
	case features.LanguageField:
		return extractors.NewLanguageExtractor()
	case features.SentimentField:
		return extractors.NewSentimentExtractor()
	case features.TextField:
		return extractors.NewTextExtractor()
	case features.NgramField:
		return extractors.NewNgramExtractor()
	case features.TitleField:
		return extractors.NewTitleExtractor()
	default:
		if registered, ok := extractors.Registered(name); ok {
			return registered.Factory()
		}
		return nil
	}
}

func (s *compositeExtractor) getExtractors() []extractors.Extractor {
	s.initOnce.Do(func() {
		names := map[string]bool{}
		for _, name := range s.Enabled {
			if names[name] {
				s.initErr = fmt.Errorf("duplicate extractor %s", name)
				return
			}
			names[name] = true
			ext := s.getExtractor(name)
			if ext == nil {
				s.initErr = fmt.Errorf("unknown extractor %s", name)
				return
			}
			s.cached = append(s.cached, ext)
		}
		for _, ext := range s.cached {
			for _, dependency := range ext.Requires() {
				if !names[dependency] {
					s.initErr = fmt.Errorf("extractor %s requires %s", ext.Name(), dependency)
					return
				}
			}
		}
	})
	return append([]extractors.Extractor(nil), s.cached...)
}

func (s *compositeExtractor) sendToTransformerQueue(composite *message.CompositeAnalysis) error {
	if s.TransformerQueue == nil {
		return nil
	}

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

	if !noAnalysis && composite.ExtractorErrors[features.UrlField] != "" {
		return fmt.Errorf("URL extraction: %s", composite.ExtractorErrors[features.UrlField])
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

func (s *compositeExtractor) OnCommit(msg types.Message) error {
	var meta message.FetcherResponse
	if err := json.Unmarshal(msg.Message, &meta); err != nil {
		return err
	}
	if meta.StoreKey == "" {
		return nil
	}
	return s.ObjectStore.Delete(meta.StoreKey)
}
