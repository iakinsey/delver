package extractor

import (
	"encoding/json"
	"github.com/iakinsey/delver/resource/objectstore"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompositeExtractionAndCommit(t *testing.T) {
	store := objectstore.NewFilesystemObjectStore(objectstore.FilesystemObjectStoreParams{Path: filepath.Join(t.TempDir(), "objects")})
	key := types.NewV4()
	_, err := store.Put(key, strings.NewReader(`<html><title>Test headline</title><body><a href="/next">next</a><p>some text here</p></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	s := NewCompositeExtractorWorker(CompositeArgs{ObjectStore: store, Enabled: []string{"url", "text", "title"}}).(*compositeExtractor)
	raw, _ := json.Marshal(message.FetcherResponse{StoreKey: key, Success: true, FetcherRequest: message.FetcherRequest{URI: "https://example.test/"}})
	input := types.Message{Message: raw}
	result, err := s.OnMessage(input)
	if err != nil {
		t.Fatal(err)
	}
	var title string
	c := result.(message.CompositeAnalysis)
	if err := c.Load("title", &title); err != nil || title != "Test headline" {
		t.Fatalf("title=%q err=%v", title, err)
	}
	f, err := store.Get(key)
	if err != nil {
		t.Fatal("body removed before commit")
	}
	io.Copy(io.Discard, f)
	f.Close()
	if err := s.OnCommit(input); err != nil {
		t.Fatal(err)
	}
	if f, err := store.Get(key); err == nil {
		f.Close()
		t.Fatal("body retained after commit")
	}
}
func TestMissingDependencies(t *testing.T) {
	s := &compositeExtractor{Enabled: []string{"sentiment"}}
	_, err := s.executeExtractors("unused", message.FetcherResponse{})
	if err == nil {
		t.Fatal("missing dependencies accepted")
	}
}
func TestFailedFetchDoesNotDeleteStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "objects")
	store := objectstore.NewFilesystemObjectStore(objectstore.FilesystemObjectStoreParams{Path: path})
	s := NewCompositeExtractorWorker(CompositeArgs{ObjectStore: store, Enabled: []string{"url"}})
	_, err := s.OnMessage(types.Message{Message: json.RawMessage(`{"success":false,"error":"network failure"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
