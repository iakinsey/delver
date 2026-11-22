package fetcher

import (
	"context"
	"encoding/json"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/resource/objectstore"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProtectionStopsBeforeStorageAndClosesConnection(t *testing.T) {
	var closed atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<p>synthetic restricted marker</p>")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		closed.Store(true)
	}))
	defer srv.Close()
	p, e := protection.Open(filepath.Join(t.TempDir(), "protection.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if e = p.Update(1, nil, []protection.Rule{{ID: "fixture", Enabled: true, Phrases: []string{"synthetic restricted marker"}}}); e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	c := config.Config{RuntimeCrawlID: "crawl", RuntimeRunID: "run", HTTPClient: config.HTTPClientConfig{Protection: p, Timeout: time.Second * 3, MaxResponseBytes: 1 << 20}}
	w := NewHttpFetcherWithConfig(HttpFetcherParams{ObjectStore: objectstore.NewFilesystemObjectStore(objectstore.FilesystemObjectStoreParams{Path: root})}, c).(*httpFetcher)
	defer w.OnComplete()
	raw, _ := json.Marshal(message.FetcherRequest{URI: srv.URL, RequestID: types.NewV4()})
	out, e := w.OnMessageContext(context.Background(), types.Message{Message: raw})
	if e != nil {
		t.Fatal(e)
	}
	response := out.(message.FetcherResponse)
	if response.Success || response.StoreKey != "" || response.FailureClass != "excluded" {
		t.Fatal(response)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("blocked response persisted", entries)
	}
	deadline := time.Now().Add(time.Second)
	for !closed.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !closed.Load() {
		t.Fatal("response connection not cancelled")
	}
}
func TestProtectionPreventsRobotsAndDestination(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		io.WriteString(w, "User-agent: *\nAllow: /\n")
	}))
	defer srv.Close()
	p, e := protection.Open(filepath.Join(t.TempDir(), "policy.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	entries, _ := protection.ParseList(srv.URL + "/blocked")
	if e = p.Update(1, []protection.List{{ID: "list", Name: "test", Enabled: true, Refresh: "6h", Updated: time.Now(), Entries: entries}}, nil); e != nil {
		t.Fatal(e)
	}
	c := config.Config{HTTPClient: config.HTTPClientConfig{Protection: p, RespectRobots: true, Timeout: time.Second}}
	w := NewHttpFetcherWithConfig(HttpFetcherParams{ObjectStore: objectstore.NewFilesystemObjectStore(objectstore.FilesystemObjectStoreParams{Path: t.TempDir()})}, c).(*httpFetcher)
	defer w.OnComplete()
	raw, _ := json.Marshal(message.FetcherRequest{URI: srv.URL + "/blocked"})
	if _, e = w.OnMessage(types.Message{Message: raw}); e != nil {
		t.Fatal(e)
	}
	if hits.Load() != 0 {
		t.Fatal("blocked URL made destination or robots request", hits.Load())
	}
}

func TestProtectionBlocksRedirectBeforeDestinationAndRedactsItsLocation(t *testing.T) {
	var hits atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); io.WriteString(w, "unreachable") }))
	defer destination.Close()
	start := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/blocked", http.StatusFound)
	}))
	defer start.Close()
	root := t.TempDir()
	p, e := protection.Open(filepath.Join(root, "policy.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	entries, e := protection.ParseList(destination.URL + "/blocked")
	if e != nil {
		t.Fatal(e)
	}
	if e = p.Update(1, []protection.List{{ID: "redirect", Name: "redirect", Enabled: true, Refresh: "6h", Updated: time.Now(), Entries: entries}}, nil); e != nil {
		t.Fatal(e)
	}
	objects := filepath.Join(root, "objects")
	c := config.Config{HTTPClient: config.HTTPClientConfig{Protection: p, Timeout: time.Second, MaxResponseBytes: 1 << 20}}
	fetch := NewHttpFetcherWithConfig(HttpFetcherParams{ObjectStore: objectstore.NewFilesystemObjectStore(objectstore.FilesystemObjectStoreParams{Path: objects})}, c)
	defer fetch.OnComplete()
	raw, _ := json.Marshal(message.FetcherRequest{URI: start.URL + "/start", RequestID: types.NewV4()})
	out, e := fetch.OnMessage(types.Message{Message: raw})
	if e != nil {
		t.Fatal(e)
	}
	result := out.(message.FetcherResponse)
	if result.FailureClass != "excluded" || result.StoreKey != "" || result.Error != protection.ErrBlocked.Error() || hits.Load() != 0 {
		t.Fatal(result, hits.Load())
	}
	files, e := os.ReadDir(objects)
	if e != nil || len(files) != 0 {
		t.Fatal("redirect committed response data", files, e)
	}
}

func TestProtectionStreamingRuleAcrossChunksAndHTMLNormalization(t *testing.T) {
	var actualUA atomic.Value
	closed := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actualUA.Store(r.UserAgent())
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<body>"+strings.Repeat(" harmless ", 8000)+"synthetic&nbsp;<b>restricted</b> ")
		w.(http.Flusher).Flush()
		io.WriteString(w, "marker</body>")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(closed)
	}))
	defer srv.Close()
	root := t.TempDir()
	p, e := protection.Open(filepath.Join(root, "policy.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if e = p.Update(1, nil, []protection.Rule{{ID: "synthetic", Enabled: true, Phrases: []string{"synthetic restricted marker"}}}); e != nil {
		t.Fatal(e)
	}
	objects := filepath.Join(root, "objects")
	c := config.Config{RuntimeCrawlID: "crawl", RuntimeRunID: "run", HTTPClient: config.HTTPClientConfig{Protection: p, Timeout: 3 * time.Second, MaxResponseBytes: 1 << 20, UserAgent: "https://en.wikipedia.org/wiki/Hatsune_Miku"}}
	fetch := NewHttpFetcherWithConfig(HttpFetcherParams{ObjectStore: objectstore.NewFilesystemObjectStore(objectstore.FilesystemObjectStoreParams{Path: objects})}, c)
	defer fetch.OnComplete()
	raw, _ := json.Marshal(message.FetcherRequest{URI: srv.URL, RequestID: types.NewV4()})
	out, e := fetch.OnMessage(types.Message{Message: raw})
	if e != nil {
		t.Fatal(e)
	}
	response := out.(message.FetcherResponse)
	if response.FailureClass != "excluded" || response.StoreKey != "" || actualUA.Load() != c.HTTPClient.UserAgent {
		t.Fatal(response, actualUA.Load())
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("stream not cancelled")
	}
	files, e := os.ReadDir(objects)
	if e != nil || len(files) != 0 {
		t.Fatal(files, e)
	}
}
