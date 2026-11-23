package extractors

import (
	"context"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/trap"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPreparedAnalysisMatchesIndependentExtractors(t *testing.T) {
	for _, body := range []string{"<html><base href='https://target.invalid/'><a href='/x'>hi</a></html>", "<script><a href='wrong'>text</script><a href='a'>entity&amp;</a>", strings.Repeat("<div><a href='/x'>x</a></div>", 30000), strings.Repeat("x", trap.ObservationByteLimit) + "<a href='/last'>x</a>", "<table><div><a href='/broken'>x"} {
		path := filepath.Join(t.TempDir(), "page")
		if e := os.WriteFile(path, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
		run := func(ctx context.Context) (interface{}, interface{}) {
			t.Helper()
			f, e := os.Open(path)
			if e != nil {
				t.Fatal(e)
			}
			defer f.Close()
			c := message.CompositeAnalysis{FetcherResponse: message.FetcherResponse{FetcherRequest: message.FetcherRequest{URI: "http://source.invalid/original"}, EffectiveURI: "http://effective.invalid/path"}, Features: map[string]interface{}{}}
			u, e := NewUrlExtractor().(interface {
				PerformContext(context.Context, *os.File, message.CompositeAnalysis) (interface{}, error)
			}).PerformContext(ctx, f, c)
			if e != nil {
				t.Fatal(e)
			}
			c.Features[features.UrlField] = u
			a, e := NewAdversarialExtractorWithConfig(config.LoadConfig()).(*adversarialExtractor).PerformContext(ctx, f, c)
			if e != nil {
				t.Fatal(e)
			}
			return u, a
		}
		a, b := run(context.Background())
		c, d := run(WithPreparedAnalysis(context.Background()))
		if !reflect.DeepEqual(a, c) || !reflect.DeepEqual(b, d) {
			t.Fatal("shared analysis changed output")
		}
	}
}
func TestPreparedAnalysisCustomFallbackAndCancellation(t *testing.T) {
	ctx := WithPreparedAnalysis(context.Background())
	path := filepath.Join(t.TempDir(), "page")
	os.WriteFile(path, []byte("<p>fixture</p>"), 0600)
	f, _ := os.Open(path)
	defer f.Close()
	c := message.CompositeAnalysis{FetcherResponse: message.FetcherResponse{FetcherRequest: message.FetcherRequest{URI: "http://fixture.invalid/"}}, Features: map[string]interface{}{features.UrlField: features.URIs{"http://other.invalid/"}}}
	a, e := NewAdversarialExtractorWithConfig(config.LoadConfig()).(*adversarialExtractor).PerformContext(ctx, f, c)
	if e != nil || a.(features.Adversarial).Structure == nil {
		t.Fatal(a, e)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = NewUrlExtractor().(*urlExtractor).PerformContext(cancelled, f, c); e != context.Canceled {
		t.Fatal(e)
	}
	if prepared(ctx).structure != nil {
		t.Fatal("unexpected cache from fallback")
	}
}
