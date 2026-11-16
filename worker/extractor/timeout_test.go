package extractor

import (
	"context"
	"github.com/iakinsey/delver/extractors"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
	"time"
)

type blockedExtractor struct{ release chan struct{} }

func (s *blockedExtractor) Name() string       { return "blocked" }
func (s *blockedExtractor) Requires() []string { return nil }
func (s *blockedExtractor) Perform(*os.File, message.CompositeAnalysis) (interface{}, error) {
	<-s.release
	return "done", nil
}
func TestExtractorTimeoutRetainsLegacyExecution(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "body")
	require.NoError(t, err)
	f.Close()
	ext := &blockedExtractor{release: make(chan struct{})}
	worker := &compositeExtractor{Timeout: 10 * time.Millisecond}
	_, errs := worker.executeExtractorSet([]extractors.Extractor{ext}, f.Name(), &message.CompositeAnalysis{Features: map[string]interface{}{}})
	require.Len(t, errs, 1)
	require.ErrorIs(t, errs[0], context.DeadlineExceeded)
	stopped := make(chan struct{})
	go func() { worker.OnComplete(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("cleanup ran while extractor was active")
	case <-time.After(10 * time.Millisecond):
	}
	close(ext.release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not finish")
	}
}

type panickingExtractor struct{}

func (panickingExtractor) Name() string       { return "url" }
func (panickingExtractor) Requires() []string { return nil }
func (panickingExtractor) Perform(*os.File, message.CompositeAnalysis) (interface{}, error) {
	panic("injected extension panic")
}
func TestExtractorPanicBecomesAnError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "body")
	require.NoError(t, err)
	f.Close()
	worker := &compositeExtractor{Timeout: time.Second}
	analysis := &message.CompositeAnalysis{Features: map[string]interface{}{}}
	_, errs := worker.executeExtractorSet([]extractors.Extractor{panickingExtractor{}}, f.Name(), analysis)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0].Error(), "injected extension panic")
	require.Error(t, getCompositeError(analysis, errs))
	worker.OnComplete()
}
