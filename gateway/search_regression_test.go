package gateway

import (
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/types"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIndexManyReportsItemFailure(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/" {
			fmt.Fprint(w, `{"version":{"number":"7.17.1"},"tagline":"You Know, for Search"}`)
			return
		}
		calls++
		fmt.Fprint(w, `{"errors":true,"items":[{"index":{"status":429,"error":{"type":"rejected"}}}]}`)
	}))
	defer server.Close()
	search := NewSearchGateway([]string{server.URL})
	if err := search.IndexMany([]*types.Indexable{{ID: "test", Index: "composite", Data: map[string]interface{}{"uri": "https://example.test"}}}); err == nil {
		t.Fatal("bulk item error ignored")
	}
	if calls != 1 {
		t.Fatalf("bulk requests: %d", calls)
	}
}
func TestAggregateCardinalityError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		if r.URL.Path == "/" {
			fmt.Fprint(w, `{"version":{"number":"7.17.1"},"tagline":"You Know, for Search"}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"aggregations": map[string]interface{}{}})
	}))
	defer server.Close()
	search := NewSearchGateway([]string{server.URL})
	if _, err := search.SearchAggregate(nil); err == nil {
		t.Fatal("missing aggregation reported success")
	}
}
