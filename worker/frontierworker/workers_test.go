package frontierworker

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"path/filepath"
	"testing"
)

func TestWorkers(t *testing.T) {
	s, err := frontier.OpenStore(frontier.StoreParams{Path: filepath.Join(t.TempDir(), "frontier")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	q, err := queue.NewCrawlQueue(queue.CrawlQueueParams{Path: filepath.Join(t.TempDir(), "queue"), CrawlID: "crawl", MaxAttempts: 3, NotifyDispatch: true})
	if err != nil {
		t.Fatal(err)
	}
	defer q.(interface{ Close() error }).Close()
	a, err := New(Params{Store: s, Scope: "open_web"}, "accumulator")
	if err != nil {
		t.Fatal(err)
	}
	c := message.CompositeAnalysis{FetcherResponse: message.FetcherResponse{FetcherRequest: message.FetcherRequest{URI: "https://a.test/", RequestID: "seed"}, Success: true, HTTPCode: 200}, Features: map[string]interface{}{features.UrlField: []string{"/one", "https://b.test/two", "mailto:no@test"}}}
	body, _ := json.Marshal(c)
	if _, err := a.OnMessage(types.Message{Message: body}); err != nil {
		t.Fatal(err)
	}
	rows, _, err := s.Pending(context.Background(), "", 10)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	p, err := New(Params{Store: s, Output: q, Strategy: "adaptive"}, "publisher")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.OnMessage(types.Message{}); err != nil {
		t.Fatal(err)
	}
	if q.Len() != 2 {
		t.Fatal(q.Len())
	}
	rows, _, err = s.Pending(context.Background(), "", 10)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}

type failingQueue struct {
	queue.Queue
	calls  int
	failAt int
}

func (q *failingQueue) Put(m types.Message, p int) error {
	q.calls++
	if q.calls == q.failAt {
		return fmt.Errorf("injected publication failure")
	}
	return q.Queue.Put(m, p)
}
func TestPublicationReplay(t *testing.T) {
	s, err := frontier.OpenStore(frontier.StoreParams{Path: filepath.Join(t.TempDir(), "frontier")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for _, uri := range []string{"https://a.test/", "https://b.test/"} {
		if err := s.Admit(ctx, message.FetcherRequest{URI: uri}); err != nil {
			t.Fatal(err)
		}
	}
	q, err := queue.NewCrawlQueue(queue.CrawlQueueParams{Path: filepath.Join(t.TempDir(), "queue"), CrawlID: "test", MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer q.(interface{ Close() error }).Close()
	faulty := &failingQueue{Queue: q, failAt: 2}
	w, err := New(Params{Store: s, Output: faulty}, "publisher")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.OnMessage(types.Message{}); err == nil {
		t.Fatal("expected publication failure")
	}
	rows, _, err := s.Pending(ctx, "", 10)
	if err != nil || len(rows) != 1 || rows[0].State != "reserved" {
		t.Fatal(rows, err)
	}
	id := rows[0].Request.RequestID
	if _, err := w.OnMessage(types.Message{}); err != nil {
		t.Fatal(err)
	}
	if q.Len() != 2 {
		t.Fatal(q.Len())
	}
	if err := s.Outcome(message.FetcherResponse{FetcherRequest: message.FetcherRequest{URI: rows[0].Request.URI, RequestID: id}, Success: true}); err != nil {
		t.Fatal(err)
	}
}

func TestScopeDepthHopAndPatternPolicies(t *testing.T) {
	for _, tc := range []struct {
		name, scope, allow, deny string
		depth, hops, want        int
	}{
		{"origin", "same_origin", "", "", 0, 0, 1},
		{"domain", "same_domain", "", "", 0, 0, 3},
		{"web", "open_web", "", "", 0, 0, 4},
		{"depth_limit", "same_domain", "", "", 1, 0, 0},
		{"hop_limit", "open_web", "", "", 0, 3, 3},
		{"patterns", "open_web", "keep", "other", 0, 0, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := frontier.OpenStore(frontier.StoreParams{Path: filepath.Join(t.TempDir(), "db")})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			w, err := New(Params{Store: s, Scope: tc.scope, MaxDepth: 1, MaxHops: 3, AllowPattern: tc.allow, DenyPattern: tc.deny}, "accumulator")
			if err != nil {
				t.Fatal(err)
			}
			c := message.CompositeAnalysis{FetcherResponse: message.FetcherResponse{FetcherRequest: message.FetcherRequest{URI: "https://a.example.test/", Depth: tc.depth, Hops: tc.hops}, Success: true}, Features: map[string]interface{}{features.UrlField: []string{"/keep", "http://a.example.test/keep", "https://b.example.test/keep", "https://other.test/keep"}}}
			b, _ := json.Marshal(c)
			if _, err := w.OnMessage(types.Message{Message: b}); err != nil {
				t.Fatal(err)
			}
			rows, _, err := s.Pending(context.Background(), "", 10)
			if err != nil || len(rows) != tc.want {
				t.Fatal(len(rows), err)
			}
		})
	}
}
