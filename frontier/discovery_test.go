package frontier

import (
	"context"
	"fmt"
	"github.com/iakinsey/delver/types/message"
	"path/filepath"
	"testing"
)

func TestDiscoveryBackpressureRestartAndReplay(t *testing.T) {
	ctx := context.Background()
	p := StoreParams{Path: filepath.Join(t.TempDir(), "db"), MaxPending: 1}
	s, err := OpenStore(p)
	if err != nil {
		t.Fatal(err)
	}
	rs := []message.FetcherRequest{{URI: "https://a.test/"}, {URI: "https://b.test/"}, {URI: "https://c.test/"}}
	if err := s.StageDiscovery(ctx, "source", rs); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DrainDiscovery(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := s.StageDiscovery(ctx, "source", rs); err != nil {
		t.Fatal(err)
	}
	counts, _ := s.Counts()
	if counts["pending_and_active"] != 1 || counts["deferred_discoveries"] != 2 {
		t.Fatal(counts)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 3; i++ {
		rows, _, err := s.Pending(ctx, "", 10)
		if err != nil || len(rows) != 1 {
			t.Fatal(rows, err)
		}
		if err := s.Outcome(message.FetcherResponse{FetcherRequest: rows[0].Request, Success: true, HTTPCode: 200}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DrainDiscovery(ctx, 10); err != nil {
			t.Fatal(err)
		}
	}
	counts, _ = s.Counts()
	if counts["pending_and_active"] != 0 || counts["deferred_discoveries"] != 0 || counts["admitted"] != 3 {
		t.Fatal(counts)
	}
}

func TestDiscoveryChunksReplayAndBudgetRejection(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(StoreParams{Path: filepath.Join(t.TempDir(), "db"), MaxURLs: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	requests := []message.FetcherRequest{}
	for i := 0; i < 130; i++ {
		requests = append(requests, message.FetcherRequest{URI: fmt.Sprintf("https://a.test/%d", i)})
	}
	if err := s.StageDiscovery(ctx, "chunked", requests); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DrainDiscovery(ctx, 128); err != nil {
		t.Fatal(err)
	}
	if err := s.StageDiscovery(ctx, "chunked", requests); err != nil {
		t.Fatal(err)
	}
	counts, _ := s.Counts()
	if counts["deferred_discoveries"] != 2 {
		t.Fatal(counts)
	}
	if _, err := s.DrainDiscovery(ctx, 128); err != nil {
		t.Fatal(err)
	}
	counts, _ = s.Counts()
	if counts["deferred_discoveries"] != 0 || counts["admitted"] != 1 || counts["budget_rejected"] != 129 {
		t.Fatal(counts)
	}
}
