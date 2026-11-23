package queue

import (
	"encoding/json"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"strings"
	"testing"
	"time"
)

func TestRequestQueueCacheBudget(t *testing.T) {
	for _, budget := range []int{-1, 1, 15, 1025} {
		if q, e := NewCrawlQueue(CrawlQueueParams{Path: t.TempDir(), CrawlID: "fixture", MaxAttempts: 3, CacheBudgetMB: budget}); e == nil {
			q.(*crawlQueue).Close()
			t.Fatal("accepted", budget)
		}
	}
	for _, budget := range []int{0, 16, 64, 1024} {
		q, e := NewCrawlQueue(CrawlQueueParams{Path: t.TempDir(), CrawlID: "fixture", MaxAttempts: 3, CacheBudgetMB: budget})
		if e != nil {
			t.Fatal(e)
		}
		s := q.(*crawlQueue)
		options := s.db.Opts()
		want := budget
		if want == 0 {
			want = 64
		}
		if options.BlockCacheSize+options.IndexCacheSize != int64(want)<<20 || options.NumMemtables != 2 || !options.SyncWrites {
			t.Fatal(options)
		}
		if options.MemTableSize != badger.DefaultOptions("").MemTableSize {
			t.Fatal("transaction limit changed")
		}
		if e = q.(*crawlQueue).Close(); e != nil {
			t.Fatal(e)
		}
	}
}

func TestRequestQueueCacheChangesPreserveLargePendingMessages(t *testing.T) {
	path := t.TempDir()
	q, e := NewCrawlQueue(CrawlQueueParams{Path: path, CrawlID: "fixture", MaxAttempts: 3, CacheBudgetMB: 16})
	if e != nil {
		t.Fatal(e)
	}
	request := message.FetcherRequest{URI: "http://fixture.invalid/page", ScopeRoot: strings.Repeat("fixture", 600000)}
	raw, _ := json.Marshal(request)
	if e = q.Put(types.Message{ID: "stable-message", MessageType: types.FetcherRequestType, Message: raw}, 0); e != nil {
		t.Fatal(e)
	}
	if e = q.(*crawlQueue).Close(); e != nil {
		t.Fatal(e)
	}
	q, e = NewCrawlQueue(CrawlQueueParams{Path: path, CrawlID: "fixture", MaxAttempts: 3, CacheBudgetMB: 1024})
	if e != nil {
		t.Fatal(e)
	}
	defer q.(*crawlQueue).Close()
	if e = q.Start(); e != nil {
		t.Fatal(e)
	}
	select {
	case delivered := <-q.GetChannel():
		var got message.FetcherRequest
		if e = json.Unmarshal(delivered.Message, &got); e != nil {
			t.Fatal(e)
		}
		if delivered.ID != "stable-message" || got.URI != request.URI || got.ScopeRoot != request.ScopeRoot {
			t.Fatal("pending message changed after cache reconfiguration")
		}
		if e = q.EndTransaction(delivered, true); e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pending message did not resume")
	}
}
