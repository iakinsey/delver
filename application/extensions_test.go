package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestExtensionPolicyGuidedAndCustomPipelines(t *testing.T) {
	for _, mode := range []string{"fixed", "adaptive"} {
		for _, custom := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/custom=%t", mode, custom), func(t *testing.T) {
				var mu sync.Mutex
				hits := map[string]int{}
				site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					hits[r.URL.Path]++
					mu.Unlock()
					w.Header().Set("Content-Type", "text/html")
					switch r.URL.Path {
					case "/root":
						fmt.Fprint(w, `<a href="/doc.PDF">pdf</a><a href="/archive.tar.gz">gz</a><a href="/allowed.zip.html">html</a><a href="/redirect">redirect</a>`)
					case "/redirect":
						http.Redirect(w, r, "/target.zip", 302)
					default:
						fmt.Fprint(w, "<p>fixture</p>")
					}
				}))
				defer site.Close()
				w, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
				require.NoError(t, err)
				defer w.Close()
				d := DefaultCrawlDefinition()
				d.Name = "Extension policy"
				d.Seeds = []string{site.URL + "/root", site.URL + "/seed.zip"}
				d.ExcludedExtensions = "zip, .PDF, tar.gz"
				d.Network.RespectRobots = boolPointer(false)
				d.Termination.WhenDrained = true
				d.RequestDelay = "0s"
				d.Fetch.Mode = mode
				d.Fetch.Concurrency = 4
				d.Fetch.Maximum = 8
				record := createWorkspaceCrawl(t, w, d)
				if custom {
					app, err := CompileCrawl(t.TempDir(), "custom", d)
					require.NoError(t, err)
					// Only the HTTP policy is declared: common preparation must propagate it to
					// standard frontier resources and preserve the rest of the custom pipeline.
					for i, r := range app.Resources {
						if r.Type == "frontier_store" {
							var p map[string]json.RawMessage
							require.NoError(t, json.Unmarshal(r.Parameters, &p))
							delete(p, "excluded_extensions")
							app.Resources[i].Parameters, _ = json.Marshal(p)
						}
					}
					raw, err := json.Marshal(app)
					require.NoError(t, err)
					prepared, err := Decode(strings.NewReader(string(raw)))
					require.NoError(t, err)
					require.NoError(t, Validate(prepared))
					var pipeline config.Application
					require.NoError(t, json.Unmarshal(raw, &pipeline))
					record = workspaceCall(t, w, "draft", map[string]interface{}{"crawl_id": record["id"], "revision": record["revision"], "configuration_json": string(raw)}).(map[string]interface{})
				}
				startWorkspaceCrawl(t, w, record)
				final := waitWorkspace(t, w, record["id"].(string), func(v map[string]interface{}) bool { return latestRun(v)["state"] == "completed" })
				mu.Lock()
				defer mu.Unlock()
				require.Greater(t, hits["/root"], 0)
				require.Greater(t, hits["/allowed.zip.html"], 0)
				for _, path := range []string{"/doc.PDF", "/archive.tar.gz", "/seed.zip", "/target.zip"} {
					require.Zero(t, hits[path], path)
				}
				run := latestRun(final)
				summary := run["summary"].(map[string]interface{})
				counts := summary["frontier"].(map[string]interface{})
				require.EqualValues(t, 0, counts["failed_fetches"])
				require.Greater(t, counts["policy_skips"].(float64), float64(0))
			})
		}
	}
}

func TestExtensionPreparationRejectsMalformedCustomInputs(t *testing.T) {
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://example.test/"}
	app, err := CompileCrawl(t.TempDir(), "test", d)
	require.NoError(t, err)
	app.Config.HTTPClient.ExcludedExtensions = []string{"pdf"}
	for _, raw := range []string{"null", "{}", "[]"} {
		copy := app
		copy.Resources = append([]config.Resource(nil), app.Resources...)
		for i, r := range copy.Resources {
			if r.Type == "frontier_store" {
				copy.Resources[i].Parameters = json.RawMessage(raw)
			}
		}
		_, err := prepareApplication(copy)
		require.Error(t, err, raw)
	}
	app.Config.HTTPClient.ExcludedExtensions = []string{"*.pdf"}
	_, err = prepareApplication(app)
	require.Error(t, err)
}
