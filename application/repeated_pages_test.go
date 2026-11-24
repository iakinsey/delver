package application

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/repetition"
	"github.com/iakinsey/delver/trap"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRepeatedGuidedDefaultsCustomOptInAndExactPatch(t *testing.T) {
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://fixture.test/"}
	d.RepeatedPages = nil
	app, e := CompileCrawl(t.TempDir(), "test", d)
	require.NoError(t, e)
	var params frontier.StoreParams
	for _, r := range app.Resources {
		if r.Type == "frontier_store" {
			require.NoError(t, json.Unmarshal(r.Parameters, &params))
		}
	}
	require.NotNil(t, params.RepeatedPages)
	require.Equal(t, repetition.Default(), *params.RepeatedPages)
	// Custom omission remains off; unsupported opt-in fails preparation.
	custom := app
	for i, r := range custom.Resources {
		if r.Type == "frontier_store" {
			params.RepeatedPages = nil
			custom.Resources[i].Parameters, _ = json.Marshal(params)
		}
	}
	require.NoError(t, validateRepeatedPages(custom))
	for i, worker := range app.Workers {
		if worker.Type == "composite_extractor" {
			app.Workers[i].Type = "http_fetcher"
		}
	}
	// Resource slices share backing storage with custom: explicitly restore opt-in.
	for i, r := range app.Resources {
		if r.Type == "frontier_store" {
			p := repetition.Default()
			params.RepeatedPages = &p
			app.Resources[i].Parameters, _ = json.Marshal(params)
		}
	}
	require.Error(t, validateRepeatedPages(app))
	pipeline := `{"config":{},"resources":[{"name":"frontier","type":"frontier_store","parameters":{"path":"/unused","opaque":9007199254740993123}}],"workers":[]}`
	raw, _ := json.Marshal(map[string]interface{}{"configuration_json": pipeline, "settings": map[string]interface{}{"frontier": map[string]interface{}{"mode": "off"}}})
	// Unknown resource params are rejected rather than flattened or lost.
	_, e = patchRepeatedPages(raw)
	require.Error(t, e)
	d.RepeatedPages = nil
	valid, e := CompileCrawl(t.TempDir(), "patch", d)
	require.NoError(t, e)
	for i, r := range valid.Resources {
		if r.Type == "frontier_store" {
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(r.Parameters, &fields))
			fields["max_dispatches"] = json.RawMessage(`9007199254740993123`)
			valid.Resources[i].Parameters, _ = json.Marshal(fields)
		}
	}
	text, _ := json.Marshal(valid)
	raw, _ = json.Marshal(map[string]interface{}{"configuration_json": string(text), "settings": map[string]interface{}{"frontier": map[string]interface{}{"mode": "observe"}}})
	patched, e := patchRepeatedPages(raw)
	require.NoError(t, e)
	require.Contains(t, patched.(map[string]interface{})["configuration_json"], "9007199254740993123")
}
func TestRepeatedStoredAnalysisOperationAndRevisionConflict(t *testing.T) {
	ctx := context.Background()
	w, e := NewWorkspace(ctx, Options{DataDirectory: t.TempDir()})
	require.NoError(t, e)
	defer w.Close()
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://seed.test/"}
	noRobots := false
	d.Network.RespectRobots = &noRobots
	record := createWorkspaceCrawl(t, w, d)
	id := record["id"].(string)
	runID := "stored-results"
	app, e := CompileCrawl(w.runRoot(id, runID), runID, d)
	require.NoError(t, e)
	var params frontier.StoreParams
	for _, r := range app.Resources {
		if r.Type == "frontier_store" {
			require.NoError(t, json.Unmarshal(r.Parameters, &params))
		}
	}
	store, e := frontier.OpenStore(params)
	require.NoError(t, e)
	require.NoError(t, store.Close())
	w.mu.Lock()
	w.records[id].Runs = []CrawlRun{{ID: runID, Definition: d, State: "stopped", Desired: "stopped"}}
	require.NoError(t, w.saveLocked())
	w.mu.Unlock()
	run := CrawlRun{ID: runID, Definition: d}
	path, e := resultPath(run, w.runRoot(id, runID))
	require.NoError(t, e)
	file, e := os.Create(path)
	require.NoError(t, e)
	signature := make(trap.HashVector, 64)
	for i := range signature {
		signature[i] = uint64(i + 1)
	}
	for i := 0; i < 3; i++ {
		uri := fmt.Sprintf("http://mirror-%d.test/", i)
		links := features.URIs{"http://destination.test/"}
		c := message.CompositeAnalysis{FetcherResponse: message.FetcherResponse{FetcherRequest: message.FetcherRequest{URI: uri, RequestID: types.UUID(fmt.Sprint(i))}, Success: true, HTTPCode: 200, Timestamp: 1}, Features: map[string]interface{}{features.UrlField: links, features.TextField: strings.Repeat("shared content apple banana peach plum ", 30), features.AdversarialField: features.Adversarial{Structure: &trap.Observation{Signature: signature, Tokens: 80}}}}
		body, _ := json.Marshal(c)
		envelope, _ := json.Marshal(types.Message{ID: fmt.Sprint(i), MessageType: types.CompositeAnalysisType, Message: body})
		_, e = file.Write(append(envelope, '\n'))
		require.NoError(t, e)
	}
	require.NoError(t, file.Close())
	raw, _ := json.Marshal(map[string]interface{}{"crawl_id": id, "revision": 0, "id": "conflict"})
	_, e = w.HandleCrawls(ctx, "repeated_analyze", raw)
	require.Error(t, e)
	op := workspaceCall(t, w, "repeated_analyze", map[string]interface{}{"crawl_id": id, "run_id": runID, "revision": 1, "id": "analysis"}).(map[string]interface{})
	require.Equal(t, "analysis", op["id"])
	deadline := time.Now().Add(5 * time.Second)
	for {
		view := workspaceCall(t, w, "operations", map[string]interface{}{"crawl_id": id}).(map[string]interface{})
		var job map[string]interface{}
		for _, v := range view {
			candidate := v.(map[string]interface{})
			if candidate["id"] == "analysis" {
				job = candidate
			}
		}
		if job["state"] == "completed" {
			require.EqualValues(t, 3, job["analysis"].(map[string]interface{})["records"])
			break
		}
		if job["state"] == "failed" {
			t.Fatal(job)
		}
		if time.Now().After(deadline) {
			t.Fatal("analysis timed out", job)
		}
		time.Sleep(10 * time.Millisecond)
	}
	families := workspaceCall(t, w, "repeated_families", map[string]interface{}{"crawl_id": id, "run_id": runID}).(map[string]interface{})["families"].([]interface{})
	require.Len(t, families, 1)
	require.Equal(t, true, families[0].(map[string]interface{})["confirmed"])
}

func TestRepeatedBoundedAnalysisReaderCancellationAndOversize(t *testing.T) {
	ctx := context.Background()
	reader := bufio.NewReaderSize(strings.NewReader(strings.Repeat("x", 9<<20)+"\nvalid\n"), 64<<10)
	line, n, err := boundedAnalysisLine(ctx, reader)
	require.NoError(t, err)
	require.Nil(t, line)
	require.EqualValues(t, (9<<20)+1, n)
	line, n, err = boundedAnalysisLine(ctx, reader)
	require.NoError(t, err)
	require.Equal(t, "valid\n", string(line))
	require.EqualValues(t, 6, n)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = boundedAnalysisLine(cancelled, reader)
	require.Equal(t, context.Canceled, err)
}

func TestRepeatedRejectsMultipleProjectionAuthorities(t *testing.T) {
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://fixture.test/"}
	app, err := CompileCrawl(t.TempDir(), "multiple", d)
	require.NoError(t, err)
	var duplicate config.Resource
	for _, r := range app.Resources {
		if r.Type == "frontier_store" {
			duplicate = r
			duplicate.Name = "other-frontier"
		}
	}
	app.Resources = append(app.Resources, duplicate)
	// Both branches otherwise provide the required features and accumulator.
	for _, worker := range append([]config.Worker{}, app.Workers...) {
		if worker.Type == "composite_extractor" {
			worker.Name = "other-extractor"
			worker.Outbox = "other-analysis"
			app.Workers = append(app.Workers, worker)
		}
		if worker.Type == "frontier_accumulator" {
			worker.Name = "other-accumulator"
			worker.Inbox = "other-analysis"
			worker.Parameters = json.RawMessage(`{"frontier_store":"other-frontier"}`)
			app.Workers = append(app.Workers, worker)
		}
	}
	require.Error(t, validateRepeatedPages(app))
	for i, r := range app.Resources {
		if r.Type == "frontier_store" {
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(r.Parameters, &fields))
			delete(fields, "repeated_pages")
			app.Resources[i].Parameters, _ = json.Marshal(fields)
		}
	}
	require.NoError(t, validateRepeatedPages(app), "custom omission must retain existing multi-frontier pipelines")
}
