package gateway

import (
	"encoding/json"
	"github.com/iakinsey/delver/filter"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/types/rpc"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveElasticsearch(t *testing.T) {
	address := strings.TrimSuffix(os.Getenv("DELVER_ES_URL"), "/")
	if address == "" {
		t.Skip("set DELVER_ES_URL to test a live cluster")
	}
	httpClient := &http.Client{Timeout: 10 * time.Second}
	rest := func(method, path string, body io.Reader) []byte {
		t.Helper()
		req, err := http.NewRequest(method, address+path, body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := httpClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode >= 300 {
			t.Fatalf("%s %s: %s", method, path, data)
		}
		return data
	}
	var cluster struct {
		Version struct {
			Number string `json:"number"`
		} `json:"version"`
	}
	json.Unmarshal(rest(http.MethodGet, "/", nil), &cluster)
	t.Logf("Elasticsearch %s", cluster.Version.Number)
	prefix := "delver-live-" + string(types.NewV4()) + "-"
	names := []string{prefix + "composite", prefix + "metric", prefix + "unrelated"}
	t.Cleanup(func() {
		for _, name := range names {
			req, _ := http.NewRequest(http.MethodDelete, address+"/"+name, nil)
			res, err := httpClient.Do(req)
			if err != nil {
				t.Errorf("cleanup %s: %v", name, err)
				continue
			}
			res.Body.Close()
			if res.StatusCode >= 300 && res.StatusCode != 404 {
				t.Errorf("cleanup %s: status %d", name, res.StatusCode)
			}
		}
	})
	search := NewSearchGateway([]string{address}).(*searchGateway)
	search.compositeIndex = names[0]
	search.metricIndex = names[1]
	for _, index := range types.Indices {
		var definition map[string]interface{}
		json.Unmarshal([]byte(index.Spec), &definition)
		definition["settings"] = map[string]interface{}{"number_of_shards": 1, "number_of_replicas": 0}
		spec, _ := json.Marshal(definition)
		name := search.compositeIndex
		if index.Name == types.MetricIndexable {
			name = search.metricIndex
		}
		search.Declare(types.Index{Name: name, Spec: string(spec)})
		search.Declare(types.Index{Name: name, Spec: string(spec)})
	}
	rest(http.MethodPut, "/"+names[2], strings.NewReader(`{"settings":{"number_of_replicas":0},"mappings":{"properties":{"timestamp":{"type":"keyword"}}}}`))
	rest(http.MethodPut, "/"+names[2]+"/_doc/noise", strings.NewReader(`{"timestamp":"unrelated","features":{"title":"A literal headline"}}`))
	now := time.Now().Unix()
	uri := "https://example.test/" + prefix
	makeAnalysis := func(id string, score float64, timestamp int64) message.CompositeAnalysis {
		return message.CompositeAnalysis{FetcherResponse: message.FetcherResponse{FetcherRequest: message.FetcherRequest{RequestID: types.UUID(id), URI: uri, Host: "example.test"}, Success: true, HTTPCode: 200, Timestamp: timestamp}, Features: map[string]interface{}{"title": "A literal headline", "text": `A literal "quote" and a*b? café`, "country": []string{"USA"}, "company_name": []string{"EXAMPLE"}, "language": map[string]interface{}{"name": "en"}, "sentiment": map[string]interface{}{"binary_naive_bayes_title": score}}}
	}
	first := makeAnalysis(prefix+"document", 0, now)
	base := time.Now().Truncate(time.Minute).Add(-2 * time.Minute).Unix()
	metricKey := prefix + ` "quoted"`
	entries := []*types.Indexable{{ID: string(first.RequestID), Index: search.compositeIndex, Data: first}, {ID: prefix + "metric-1", Index: search.metricIndex, Data: types.Metric{Key: metricKey, When: base + 10, Value: 2.5}}, {ID: prefix + "metric-2", Index: search.metricIndex, Data: types.Metric{Key: metricKey, When: base + 20, Value: 3.5}}}
	if err := search.IndexMany(entries); err != nil {
		t.Fatal(err)
	}
	if err := search.IndexMany(entries); err != nil {
		t.Fatal(err)
	}
	if err := search.Index(&types.Indexable{ID: prefix + "single", Index: search.metricIndex, Data: types.Metric{Key: prefix + "single", When: now, Value: 0.25}}); err != nil {
		t.Fatal(err)
	}
	if err := search.IndexMany([]*types.Indexable{{ID: prefix + "bad", Index: search.metricIndex, Data: map[string]interface{}{"key": "invalid", "when": now, "value": "not-a-number"}}}); err == nil {
		t.Fatal("per-item mapping failure ignored")
	}
	rest(http.MethodPost, "/"+strings.Join(names, ",")+"/_refresh", nil)
	t.Run("composite filters and index isolation", func(t *testing.T) {
		params := rpc.FilterParams{Fields: []string{"uri", "timestamp", "features"}, Query: rpc.CompositeFilterQuery{Keyword: []string{`LITERAL "QUOTE"`, `a*b?`, `café`}, Country: []string{"USA"}, Company: []string{"EXAMPLE"}, Url: []string{uri}, Domain: []string{"example.test"}, Title: []string{"A literal headline"}, Language: []string{"en"}, HttpCode: []int{200}}}
		query, err := filter.NewCompositeSearchFilter(params).Perform()
		if err != nil {
			t.Fatal(err)
		}
		results, err := search.Search(query)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 {
			t.Fatalf("want one deduplicated document, got %d", len(results))
		}
		params.Query = rpc.CompositeFilterQuery{Country: []string{"GBR"}}
		query, _ = filter.NewCompositeSearchFilter(params).Perform()
		results, err = search.Search(query)
		if err != nil || len(results) != 0 {
			t.Fatalf("negative filter: %d results, %v", len(results), err)
		}
	})
	t.Run("metric sum average and seconds", func(t *testing.T) {
		for name, wanted := range map[string]float64{"sum": 6, "avg": 3} {
			f := filter.NewMetricSearchFilter(rpc.FilterParams{Query: rpc.MetricFilterQuery{Key: metricKey, Start: base, End: base + 59}, Agg: &rpc.Aggregator{Name: name, TimeWindowSeconds: 60}})
			query, err := f.Perform()
			if err != nil {
				t.Fatal(err)
			}
			raw, err := search.SearchAggregate(query)
			if err != nil {
				t.Fatal(err)
			}
			result, err := f.Postprocess(raw)
			if err != nil || len(result) != 1 {
				t.Fatalf("%s aggregation: %v %v", name, result, err)
			}
			var point types.Metric
			json.Unmarshal(result[0], &point)
			if point.Value != wanted || point.When != base || point.Samples != 2 {
				t.Fatalf("%s: %+v", name, point)
			}
		}
	})
}
