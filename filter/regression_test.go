package filter

import (
	"encoding/json"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/types/rpc"
	"io"
	"strings"
	"testing"
	"time"
)

func TestRegressionMetricStream(t *testing.T) {
	f := NewMetricStreamFilter(rpc.FilterParams{Query: rpc.MetricFilterQuery{Key: "fetch", Start: time.Now().Add(-time.Hour).Unix(), End: time.Now().Add(time.Hour).Unix()}})
	r, err := f.Perform([]*types.Indexable{{Data: types.Metric{Key: "fetch", When: time.Now().Unix(), Value: 1}}})
	if err != nil || len(r) != 1 {
		t.Fatalf("expected one matching metric; got count=%d error=%v", len(r), err)
	}
}
func TestRegressionCompositeCountry(t *testing.T) {
	s := &compositeStreamFilter{CompositeFilterQuery: rpc.CompositeFilterQuery{Country: []string{"US"}}}
	c := message.CompositeAnalysis{Features: map[string]interface{}{"country": []string{"FR"}}}
	if s.filterCountry(c) {
		t.Fatal("US country filter accepts FR-only document")
	}
}
func TestRegressionCompositeDefaultFields(t *testing.T) {
	s := &compositeStreamFilter{}
	c := message.CompositeAnalysis{Features: map[string]interface{}{"title": "headline"}}
	c.URI = "https://example.test"
	c.Host = "example.test"
	c.HTTPCode = 200
	c.Timestamp = time.Now().Unix()
	c.ElapsedTimeMs = 2
	if !s.filterFields(c) {
		t.Fatal("default source projection incorrectly used as feature-existence filter")
	}
}
func TestRegressionCompositeSearchFieldPaths(t *testing.T) {
	f := NewCompositeSearchFilter(rpc.FilterParams{Query: rpc.CompositeFilterQuery{Domain: []string{"example.test"}, Title: []string{"headline"}, Language: []string{"en"}}})
	r, err := f.Perform()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	for _, want := range []string{`"host"`, `"features.title"`, `"features.language.name"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("query missing %s: %s", want, b)
		}
	}
}
func TestRegressionQuotedSearchInput(t *testing.T) {
	f := NewCompositeSearchFilter(rpc.FilterParams{Query: rpc.CompositeFilterQuery{Keyword: []string{`say "hello"`}}})
	r, err := f.Perform()
	if err != nil {
		t.Fatalf("valid quoted keyword fails query construction: %v", err)
	}
	b, _ := io.ReadAll(r)
	if !json.Valid(b) {
		t.Fatalf("invalid query: %s", b)
	}
}
func TestRegressionMetricIntervalSeconds(t *testing.T) {
	f := NewMetricSearchFilter(rpc.FilterParams{Query: rpc.MetricFilterQuery{Key: "fetch"}, Agg: &rpc.Aggregator{Name: "sum", TimeWindowSeconds: 60}})
	r, err := f.Perform()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	if !strings.Contains(string(b), `"fixed_interval": "60s"`) {
		t.Fatalf("60 seconds query becomes: %s", b)
	}
}
