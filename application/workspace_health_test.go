package application

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestWorkspaceFiniteFeedHealthRecoversAndDrains(t *testing.T) {
	var healthy atomic.Bool
	var site *httptest.Server
	site = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			fmt.Fprint(w, "User-agent: *\nAllow: /\n")
		case "/feed":
			if !healthy.Load() {
				fmt.Fprint(w, "invalid feed")
				return
			}
			fmt.Fprintf(w, `<rss version="2.0"><channel><title>Recovered</title><link>%s/</link><description>Recovered</description><item><title>Page</title><link>%s/article</link></item></channel></rss>`, site.URL, site.URL)
		default:
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<title>Recovered page</title><p>Recovered article body.</p>")
		}
	}))
	defer site.Close()
	workspace, err := NewWorkspace(context.Background(), Options{DataDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	definition := DefaultCrawlDefinition()
	definition.Feeds = []string{site.URL + "/feed"}
	definition.Termination.WhenDrained = true
	record := createWorkspaceCrawl(t, workspace, definition)
	id := record["id"].(string)
	startWorkspaceCrawl(t, workspace, record)
	waitWorkspace(t, workspace, id, func(record map[string]interface{}) bool {
		runtime := record["runtime"].(map[string]interface{})["runtime"].(map[string]interface{})
		health := runtime["health"].(map[string]interface{})
		return health["starting_feeds"] != "" && health["starting_feeds"] != nil
	})
	healthy.Store(true)
	final := waitWorkspace(t, workspace, id, func(record map[string]interface{}) bool { return latestRun(record)["state"] == "completed" })
	summary := latestRun(final)["summary"].(map[string]interface{})
	if len(summary["health"].(map[string]interface{})) != 0 {
		t.Fatalf("recovered source retained stale health: %v", summary["health"])
	}
	if summary["frontier"].(map[string]interface{})["terminal_outcomes"] != float64(1) {
		t.Fatal("recovered feed did not produce a result", summary)
	}
}
