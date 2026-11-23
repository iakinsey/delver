package application

import (
	"encoding/json"
	"github.com/iakinsey/delver/worker"
	"strings"
	"testing"
)

func TestRequestQueueCacheCompilationAndPatch(t *testing.T) {
	RegisterWorker("cache_fixture", func(json.RawMessage, map[string]interface{}) (worker.Worker, error) { return nil, nil })
	defer RegisterWorker("cache_fixture", nil)
	for _, budget := range []int{0, 16, 64, 1024, 15, 1025} {
		d := DefaultCrawlDefinition()
		d.Seeds = []string{"http://fixture.invalid/"}
		d.RequestQueueCacheMB = budget
		app, e := CompileCrawl(t.TempDir(), "fixture", d)
		if budget == 15 || budget == 1025 {
			if e == nil {
				t.Fatal("accepted", budget)
			}
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		want := budget
		if want == 0 {
			want = 64
		}
		for _, r := range app.Resources {
			if r.Type == "crawl_queue" {
				var raw map[string]json.RawMessage
				json.Unmarshal(r.Parameters, &raw)
				var actual int
				json.Unmarshal(raw["cache_budget_mb"], &actual)
				if actual != want {
					t.Fatal(actual, want)
				}
			}
		}
		app.Workers[len(app.Workers)-1].Type = "cache_fixture"
		app.Workers[len(app.Workers)-1].Parameters = json.RawMessage(`{"opaque":9007199254740993123}`)
		raw, _ := json.Marshal(app)
		for _, patchedBudget := range []int{16, 1024, 15, 1025} {
			request, _ := json.Marshal(map[string]interface{}{"configuration_json": string(raw), "budgets": map[string]int{"requests": patchedBudget}})
			out, e := patchQueueCache(request)
			if patchedBudget == 15 || patchedBudget == 1025 {
				if e == nil {
					t.Fatal("accepted patch", patchedBudget)
				}
				continue
			}
			if e != nil {
				t.Fatal(e)
			}
			text := out.(map[string]interface{})["configuration_json"].(string)
			if !strings.Contains(text, "9007199254740993123") {
				t.Fatal("lost integer")
			}
			if _, e = Decode(strings.NewReader(text)); e != nil {
				t.Fatal(e)
			}
		}
	}
}
