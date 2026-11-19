package frontierworker

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
	"time"
)

func TestExcludedSeedReplayCountsOnce(t *testing.T) {
	s, err := frontier.OpenStore(frontier.StoreParams{Path: filepath.Join(t.TempDir(), "frontier"), ExcludedExtensions: []string{"pdf"}, MaxPending: 1})
	require.NoError(t, err)
	defer s.Close()
	// Even a full frontier must not stage excluded seeds or count a replay twice.
	require.NoError(t, s.Admit(context.Background(), message.FetcherRequest{URI: "http://held.test/"}))
	w := &Admission{Store: s}
	raw, err := json.Marshal(message.FetcherRequest{URI: "http://seed.test/file.PDF"})
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		_, err = w.OnMessageContext(context.Background(), types.Message{ID: "seed-delivery", Message: raw})
		require.NoError(t, err)
	}
	counts, err := s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 1, counts["policy_skips"])
	require.Zero(t, counts["deferred_discoveries"])
	require.EqualValues(t, 1, counts["pending_and_active"])
}

func TestRecrawlBetterPathReleasesFrontierCapacity(t *testing.T) {
	ctx := context.Background()
	s, err := frontier.OpenStore(frontier.StoreParams{Path: filepath.Join(t.TempDir(), "frontier"), MaxPending: 1, MaxPendingPerOrigin: 1, OriginDelay: frontier.Lifetime(time.Nanosecond), RevisitAfter: frontier.Lifetime(time.Nanosecond)})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	q, err := queue.NewCrawlQueue(queue.CrawlQueueParams{Path: filepath.Join(t.TempDir(), "queue"), CrawlID: "recrawl", MaxAttempts: 3, AcceptFrontierGenerations: true})
	if err != nil {
		t.Fatal(err)
	}
	defer q.(interface{ Close() error }).Close()
	if err = q.Start(); err != nil {
		t.Fatal(err)
	}
	p, err := New(Params{Store: s, Output: q, Strategy: "breadth_first"}, "publisher")
	if err != nil {
		t.Fatal(err)
	}
	uri := "http://recrawl.test/page"
	if err = s.Admit(ctx, message.FetcherRequest{URI: uri, Depth: 4}); err != nil {
		t.Fatal(err)
	}
	previous := ""
	for round := 0; round < 2; round++ {
		if round == 1 {
			if err = s.DrainRecrawls(ctx, 32); err != nil {
				t.Fatal(err)
			}
			if err = s.Admit(ctx, message.FetcherRequest{URI: uri, Depth: 1}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = p.OnMessage(types.Message{}); err != nil {
			t.Fatal(err)
		}
		var delivered types.Message
		select {
		case delivered = <-q.GetChannel():
		case <-time.After(2 * time.Second):
			t.Fatal("request was not published")
		}
		var request message.FetcherRequest
		if err = json.Unmarshal(delivered.Message, &request); err != nil {
			t.Fatal(err)
		}
		if string(request.RequestID) == previous {
			t.Fatal("recrawl reused the previous generation's request ID")
		}
		previous = string(request.RequestID)
		if err = q.EndTransaction(delivered, true); err != nil {
			t.Fatal(err)
		}
		if err = s.Outcome(message.FetcherResponse{FetcherRequest: request, Success: true, HTTPCode: 200}); err != nil {
			t.Fatal(err)
		}
		counts, err := s.Counts()
		if err != nil {
			t.Fatal(err)
		}
		if counts["pending_and_active"] != 0 || counts["terminal_outcomes"] != int64(round+1) {
			t.Fatalf("completed recrawl retained capacity: %v", counts)
		}
	}
}

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

func TestPublisherContinuesCoolingPrefix(t *testing.T) {
	for _, mode := range []string{"fixed", "adaptive"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, err := frontier.OpenStore(frontier.StoreParams{Path: t.TempDir(), OriginDelay: frontier.Lifetime(time.Hour)})
			require.NoError(t, err)
			defer s.Close()
			for i := 0; i < 129; i++ {
				require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: fmt.Sprintf("http://cool.test/%03d", i)}))
			}
			rows, err := s.Candidates(ctx, "breadth_first", "", 1)
			require.NoError(t, err)
			c, err := s.Reserve(ctx, rows[0])
			require.NoError(t, err)
			require.NoError(t, s.Published(c))
			require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://ready.test/page"}))
			q, err := queue.NewCrawlQueue(queue.CrawlQueueParams{Path: t.TempDir(), CrawlID: "scan", MaxAttempts: 1, AcceptFrontierGenerations: true})
			require.NoError(t, err)
			defer q.(interface{ Close() error }).Close()
			require.NoError(t, q.Start())
			p := flow.Default()
			p.Mode = mode
			p.Concurrency = 4
			p.Maximum = 8
			p.CandidateWindow = 32
			p.RefillBatch = 8
			controller, err := flow.New(ctx, t.Name(), p, nil)
			require.NoError(t, err)
			defer controller.Close()
			w, err := New(Params{Store: s, Output: q, Strategy: "breadth_first"}, "publisher")
			require.NoError(t, err)
			w.SetFlow(controller, p)
			for i := 0; i < 4; i++ {
				require.NoError(t, w.publish(ctx))
				require.False(t, controller.Snapshot().Runnable)
			}
			require.NoError(t, w.publish(ctx))
			require.True(t, controller.Snapshot().Runnable)
			select {
			case m := <-q.GetChannel():
				var r message.FetcherRequest
				require.NoError(t, json.Unmarshal(m.Message, &r))
				require.Equal(t, "http://ready.test/page", r.URI)
				require.NoError(t, q.EndTransaction(m, true))
			case <-time.After(time.Second):
				t.Fatal("ready work was not dispatched within five bounded passes")
			}
		})
	}
}

func TestRediscoveredPolicySkipGetsNewQueueGeneration(t *testing.T) {
	ctx := context.Background()
	params := frontier.StoreParams{Path: t.TempDir(), OriginDelay: frontier.Lifetime(time.Nanosecond), ExcludedExtensions: []string{"pdf"}}
	s, err := frontier.OpenStore(params)
	require.NoError(t, err)
	defer func() { s.Close() }()
	q, err := queue.NewCrawlQueue(queue.CrawlQueueParams{Path: t.TempDir(), CrawlID: "policy-rediscovery", MaxAttempts: 1, AcceptFrontierGenerations: true})
	require.NoError(t, err)
	defer q.(interface{ Close() error }).Close()
	require.NoError(t, q.Start())
	publisher, err := New(Params{Store: s, Output: q, Strategy: "breadth_first"}, "publisher")
	require.NoError(t, err)
	var previous message.FetcherRequest
	for round := 0; round < 2; round++ {
		if round == 1 {
			require.NoError(t, s.Close())
			params.ExcludedExtensions = nil
			s, err = frontier.OpenStore(params)
			require.NoError(t, err)
			publisher.Store = s
		}
		require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://example.test/redirect"}))
		require.NoError(t, publisher.publish(ctx))
		var delivered types.Message
		select {
		case delivered = <-q.GetChannel():
		case <-time.After(time.Second):
			t.Fatal("rediscovered policy skip was suppressed as a completed queue generation")
		}
		var request message.FetcherRequest
		require.NoError(t, json.Unmarshal(delivered.Message, &request))
		require.NoError(t, q.EndTransaction(delivered, true))
		if round == 0 {
			previous = request
			require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: request, FailureClass: "excluded"}))
			continue
		}
		require.NotEqual(t, previous.FrontierGeneration, request.FrontierGeneration)
		require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: previous, FailureClass: "excluded"}))
		counts, err := s.Counts()
		require.NoError(t, err)
		require.EqualValues(t, 1, counts["pending_and_active"], "stale skip outcome changed the new generation")
		require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: request, Success: true, HTTPCode: 200}))
	}
	counts, err := s.Counts()
	require.NoError(t, err)
	require.Zero(t, counts["pending_and_active"])
	require.EqualValues(t, 1, counts["policy_skips"])
	require.EqualValues(t, 2, counts["terminal_outcomes"])
}
