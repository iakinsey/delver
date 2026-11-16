package extractor

import (
	"encoding/json"
	"errors"
	"github.com/iakinsey/delver/extractors"
	"github.com/iakinsey/delver/resource/objectstore"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"os"
	"path/filepath"
	"testing"
)

type auditFailExtractor struct{}

func (auditFailExtractor) Name() string       { return "audit" }
func (auditFailExtractor) Requires() []string { return nil }
func (auditFailExtractor) Perform(*os.File, message.CompositeAnalysis) (interface{}, error) {
	return nil, errors.New("extractor failed")
}
func TestRegressionExtractorErrorPropagation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "page")
	os.WriteFile(path, []byte("page"), 0600)
	s := &compositeExtractor{}
	results := make(chan compositeResult, 1)
	s.executeExtractor(auditFailExtractor{}, path, message.CompositeAnalysis{}, results)
	_, err := s.updateCompositeAnalysis(<-results, &message.CompositeAnalysis{Features: map[string]interface{}{}})
	if err == nil {
		t.Fatal("Perform error discarded before updateCompositeAnalysis")
	}
}
func TestRegressionMissingDependency(t *testing.T) {
	s := &compositeExtractor{Enabled: []string{"text", "sentiment"}}
	path := filepath.Join(t.TempDir(), "page")
	os.WriteFile(path, []byte("<p>test text</p>"), 0600)
	_, err := s.executeExtractors(path, message.FetcherResponse{})
	if err == nil {
		t.Fatal("missing title/language dependencies silently accepted")
	}
}

type auditSlowExtractor struct{ start chan struct{} }

func (auditSlowExtractor) Name() string       { return "slow" }
func (auditSlowExtractor) Requires() []string { return nil }
func (e auditSlowExtractor) Perform(_ *os.File, c message.CompositeAnalysis) (interface{}, error) {
	close(e.start)
	for i := 0; i < 10000; i++ {
		var text string
		c.Load("text", &text)
	}
	return "done", nil
}

type auditFastExtractor struct{ start chan struct{} }

func (auditFastExtractor) Name() string       { return "fast" }
func (auditFastExtractor) Requires() []string { return nil }
func (e auditFastExtractor) Perform(_ *os.File, _ message.CompositeAnalysis) (interface{}, error) {
	<-e.start
	return "done", nil
}
func TestRaceRegressionExtractorMap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "page")
	os.WriteFile(path, []byte("page"), 0600)
	start := make(chan struct{})
	s := &compositeExtractor{}
	c := &message.CompositeAnalysis{Features: map[string]interface{}{"text": "page"}}
	s.executeExtractorSet([]extractors.Extractor{auditSlowExtractor{start}, auditFastExtractor{start}}, path, c)
}

func TestRegressionFailedFetchPreservesStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "objects")
	store := objectstore.NewFilesystemObjectStore(objectstore.FilesystemObjectStoreParams{Path: path})
	s := NewCompositeExtractorWorker(CompositeArgs{ObjectStore: store, Enabled: []string{"url"}})
	raw, _ := json.Marshal(message.FetcherResponse{Success: false, Error: "network unavailable"})
	s.OnMessage(types.Message{MessageType: types.FetcherResponseType, Message: raw})
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("failed fetch removed object-store directory: %v", err)
	}
}
