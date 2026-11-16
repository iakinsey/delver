package frontier

import (
	"context"
	"github.com/iakinsey/delver/types/message"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRetentionSeekingRecovery(t *testing.T) {
	ctx := context.Background()
	p := StoreParams{Path: filepath.Join(t.TempDir(), "store"), Retention: DefaultRetention()}
	p.Retention.CompletedFor = Lifetime(time.Hour)
	p.Retention.FailedFor = Lifetime(time.Hour)
	p.Retention.DedupFor = Lifetime(2 * time.Hour)
	s, err := OpenStore(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"https://a.test/1", "https://a.test/2", "https://b.test/3"} {
		if err := s.Admit(ctx, message.FetcherRequest{URI: u}); err != nil {
			t.Fatal(err)
		}
	}
	rows, cursor, err := s.Pending(ctx, "", 1)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	c, err := s.Reserve(ctx, rows[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, _, err = s.Pending(ctx, "", 1)
	if err != nil || rows[0].State != "reserved" || rows[0].Request.RequestID != c.Request.RequestID {
		t.Fatal(rows, err)
	}
	if err := s.Published(c); err != nil {
		t.Fatal(err)
	}
	rows, _, err = s.Pending(ctx, cursor, 10)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	if err := s.Outcome(message.FetcherResponse{FetcherRequest: c.Request, Success: true, HTTPCode: 200}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cleanup(ctx, time.Now().Add(90*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.Admit(ctx, c.Request); err != nil {
		t.Fatal(err)
	}
	rows, _, _ = s.Pending(ctx, "", 10)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	if _, err := s.Cleanup(ctx, time.Now().Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.Admit(ctx, c.Request); err != nil {
		t.Fatal(err)
	}
	rows, _, _ = s.Pending(ctx, "", 10)
	if len(rows) != 3 {
		t.Fatal(rows)
	}
}
func TestNormalize(t *testing.T) {
	u, e := Normalize("../x#frag", "https://EXAMPLE.test:443/a/b")
	if e != nil || u != "https://example.test/x" {
		t.Fatal(u, e)
	}
}

func TestNeverExpiresAndBudgets(t *testing.T) {
	p := StoreParams{Path: filepath.Join(t.TempDir(), "db"), Retention: DefaultRetention(), MaxURLs: 1}
	p.Retention.CompletedFor = 0
	p.Retention.FailedFor = 0
	p.Retention.DedupFor = 0
	s, err := OpenStore(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.Admit(ctx, message.FetcherRequest{URI: "https://a.test/"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Admit(ctx, message.FetcherRequest{URI: "https://b.test/"}); err != ErrRejected {
		t.Fatal(err)
	}
	rows, _, _ := s.Pending(ctx, "", 1)
	c := rows[0]
	if err := s.Outcome(message.FetcherResponse{FetcherRequest: c.Request, Success: true}); err != nil {
		t.Fatal(err)
	}
	n, err := s.Cleanup(ctx, time.Now().Add(365*24*time.Hour))
	if err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if err := s.Admit(ctx, c.Request); err != nil {
		t.Fatal(err)
	}
	rows, _, _ = s.Pending(ctx, "", 10)
	if len(rows) != 0 {
		t.Fatal(rows)
	}
}
func TestIndexedTraversalAndCooldown(t *testing.T) {
	s, err := OpenStore(StoreParams{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for _, r := range []message.FetcherRequest{{URI: "https://a.test/deep", Depth: 5}, {URI: "https://a.test/shallow", Depth: 1}, {URI: "https://b.test/", Depth: 2}} {
		if err := s.Admit(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.Candidates(ctx, "breadth_first", "", 1)
	if err != nil || rows[0].Request.Depth != 1 {
		t.Fatal(rows, err)
	}
	if _, err := s.Reserve(ctx, rows[0]); err != nil {
		t.Fatal(err)
	}
	replayed, replayErr := s.Candidates(ctx, "depth_first", "", 1)
	if replayErr != nil || len(replayed) != 1 || replayed[0].State != "reserved" {
		t.Fatal(replayed, replayErr)
	}
	if err := s.Published(replayed[0]); err != nil {
		t.Fatal(err)
	}
	rows, err = s.Candidates(ctx, "depth_first", "", 1)
	if err != nil || rows[0].Request.Depth != 5 {
		t.Fatal(rows, err)
	}
	if _, err := s.Reserve(ctx, rows[0]); err != ErrCooling {
		t.Fatal(err)
	}
	rows, err = s.Candidates(ctx, "host_diverse", "a.test", 1)
	if err != nil || rows[0].Request.Host != "b.test" {
		t.Fatal(rows, err)
	}
}

func TestDiskHeadroomAndCapacity(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(StoreParams{Path: filepath.Join(t.TempDir(), "disk"), MinFreeBytes: ^uint64(0)})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Admit(ctx, message.FetcherRequest{URI: "https://a.test/"}); err == nil || s.Health() == "" {
		t.Fatal("disk admission must fail visibly")
	}
	bounded, err := OpenStore(StoreParams{Path: filepath.Join(t.TempDir(), "bounded"), MaxPending: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer bounded.Close()
	if err := bounded.Admit(ctx, message.FetcherRequest{URI: "https://a.test/"}); err != nil {
		t.Fatal(err)
	}
	if err := bounded.Admit(ctx, message.FetcherRequest{URI: "https://b.test/"}); err != ErrCapacity {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := bounded.Admit(ctx, message.FetcherRequest{URI: "https://c.test/"}); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestBetterHopPathProtectsReopenedRecord(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(StoreParams{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Admit(ctx, message.FetcherRequest{URI: "https://a.test/", Hops: 3, Depth: 0}); err != nil {
		t.Fatal(err)
	}
	rows, _, _ := s.Pending(ctx, "", 1)
	old := rows[0].Request
	if err := s.Outcome(message.FetcherResponse{FetcherRequest: old, Success: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Admit(ctx, message.FetcherRequest{URI: old.URI, Hops: 1, Depth: 2}); err != nil {
		t.Fatal(err)
	}
	if err := s.Outcome(message.FetcherResponse{FetcherRequest: old, Success: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cleanup(ctx, time.Now().Add(365*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	rows, _, err = s.Pending(ctx, "", 1)
	if err != nil || len(rows) != 1 || rows[0].Request.Hops != 1 || rows[0].Request.Depth != 2 {
		t.Fatal(rows, err)
	}
	counts, _ := s.Counts()
	if counts["pending_and_active"] != 1 {
		t.Fatal(counts)
	}
}
