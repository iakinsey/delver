package queue

import (
	"context"
	"encoding/json"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"path/filepath"
	"testing"
	"time"
)

func crawlRequest(t *testing.T, uri string) types.Message {
	t.Helper()
	data, err := json.Marshal(message.FetcherRequest{URI: uri, RequestID: types.NewV4()})
	if err != nil {
		t.Fatal(err)
	}
	return types.Message{ID: string(types.NewV4()), MessageType: types.FetcherRequestType, Message: data}
}
func receiveCrawl(t *testing.T, q Queue) types.Message {
	t.Helper()
	select {
	case msg := <-q.GetChannel():
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("no crawl delivery")
		return types.Message{}
	}
}
func TestCrawlQueueExactRecovery(t *testing.T) {
	params := CrawlQueueParams{Path: filepath.Join(t.TempDir(), "crawl"), CrawlID: "test", MaxAttempts: 2}
	q, err := NewCrawlQueue(params)
	if err != nil {
		t.Fatal(err)
	}
	input := crawlRequest(t, "http://EXAMPLE.test/path#one")
	if err = q.Put(input, 0); err != nil {
		t.Fatal(err)
	}
	if err = q.Put(crawlRequest(t, "http://example.test/path#two"), 0); err != nil {
		t.Fatal(err)
	}
	if q.Len() != 1 {
		t.Fatal("normalized URL not deduplicated")
	}
	q.Start()
	first := receiveCrawl(t, q)
	out := []types.Message{{ID: "stable", Message: json.RawMessage(`{}`)}}
	q.(OutputStore).SaveOutputs(first, out)
	q.(interface{ Close() error }).Close()
	q, err = NewCrawlQueue(params)
	if err != nil {
		t.Fatal(err)
	}
	defer q.(interface{ Close() error }).Close()
	q.Start()
	retry := receiveCrawl(t, q)
	saved, found, err := q.(OutputStore).LoadOutputs(retry)
	if err != nil || !found || len(saved) != 1 || saved[0].ID != "stable" {
		t.Fatalf("staged outputs lost: %v %v %v", saved, found, err)
	}
	if err = q.EndTransaction(retry, true); err != nil {
		t.Fatal(err)
	}
	q.Put(input, 0)
	if q.Len() != 0 {
		t.Fatal("completed URL was scheduled again")
	}
}
func TestCrawlQueueFailureLimitAndCancellation(t *testing.T) {
	q, err := NewCrawlQueue(CrawlQueueParams{Path: filepath.Join(t.TempDir(), "crawl"), CrawlID: "test", MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer q.(interface{ Close() error }).Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = q.(interface {
		PutContext(context.Context, types.Message, int) error
	}).PutContext(ctx, crawlRequest(t, "https://example.test/"), 0); err == nil {
		t.Fatal("cancellation ignored")
	}
	q.Put(crawlRequest(t, "https://example.test/"), 0)
	q.Start()
	msg := receiveCrawl(t, q)
	if err = q.EndTransaction(msg, false); err != nil {
		t.Fatal(err)
	}
	if q.Len() != 0 {
		t.Fatal("failed request exceeded attempts")
	}
}

func TestShallowerDiscoveryReopensCompletedURL(t *testing.T) {
	q, err := NewCrawlQueue(CrawlQueueParams{Path: filepath.Join(t.TempDir(), "crawl"), CrawlID: "depth", MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer q.(interface{ Close() error }).Close()
	deep := crawlRequest(t, "https://example.test/path")
	var request message.FetcherRequest
	json.Unmarshal(deep.Message, &request)
	request.Depth = 3
	deep.Message, _ = json.Marshal(request)
	q.Put(deep, 0)
	q.Start()
	delivered := receiveCrawl(t, q)
	q.EndTransaction(delivered, true)
	request.Depth = 1
	shallow := deep
	shallow.Message, _ = json.Marshal(request)
	if err := q.Put(shallow, 0); err != nil {
		t.Fatal(err)
	}
	retry := receiveCrawl(t, q)
	json.Unmarshal(retry.Message, &request)
	if request.Depth != 1 {
		t.Fatalf("shallower path lost: %d", request.Depth)
	}
	q.EndTransaction(retry, true)
}
