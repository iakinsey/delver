package frontier

import (
	"context"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/types/message"
	"path/filepath"
	"testing"
)

func TestSubdomainsShareSiteAdmissionBudget(t *testing.T) {
	s, e := OpenStore(StoreParams{Path: filepath.Join(t.TempDir(), "db"), MaxPendingPerOrigin: 2})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	for n := 0; n < 2; n++ {
		if e = s.Admit(ctx, message.FetcherRequest{URI: fmt.Sprintf("http://sub-%d.service.onion/", n)}); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.Admit(ctx, message.FetcherRequest{URI: "http://sub-2.service.onion/"}); e != ErrCapacity {
		t.Fatal("subdomain bypassed site limit", e)
	}
	if e = s.Admit(ctx, message.FetcherRequest{URI: "http://other.onion/"}); e != nil {
		t.Fatal(e)
	}
}
func TestSiteMigrationReplaysAndPreservesCandidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, e := OpenStore(StoreParams{Path: path})
	if e != nil {
		t.Fatal(e)
	}
	for n := 0; n < 3; n++ {
		if e = s.Admit(context.Background(), message.FetcherRequest{URI: fmt.Sprintf("http://sub-%d.service.onion/", n)}); e != nil {
			t.Fatal(e)
		}
	}
	e = s.db.Update(func(tx *badger.Txn) error {
		if e := tx.Delete([]byte("meta/site_index_v1")); e != nil {
			return e
		}
		tx.Delete([]byte("site_pending/service.onion"))
		for n := 0; n < 3; n++ {
			if e := write(tx, fmt.Sprintf("origin/sub-%d.service.onion", n), 1); e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = OpenStore(StoreParams{Path: path})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var count int
	e = s.db.View(func(tx *badger.Txn) error { return read(tx, "site_pending/service.onion", &count) })
	if e != nil || count != 3 {
		t.Fatal(count, e)
	}
	rows, e := s.Candidates(context.Background(), "host_diverse", "", 10)
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	pending, _, err := s.Pending(context.Background(), "", 10)
	if err != nil || len(pending) != 3 {
		t.Fatal(pending, err)
	}
}

func TestThousandSubdomainsHaveIdenticalSchedulingCreditAndBudget(t *testing.T) {
	for _, subdomains := range []bool{false, true} {
		t.Run(fmt.Sprint(subdomains), func(t *testing.T) {
			s, e := OpenStore(StoreParams{Path: filepath.Join(t.TempDir(), "db"), MaxPendingPerOrigin: 100, MaxURLsPerOrigin: 1000})
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			admitted := 0
			for n := 0; n < 1000; n++ {
				host := "service.onion"
				if subdomains {
					host = fmt.Sprintf("s-%d.service.onion", n)
				}
				e = s.Admit(context.Background(), message.FetcherRequest{URI: fmt.Sprintf("http://%s/%d", host, n)})
				if e == nil {
					admitted++
				} else if e != ErrCapacity {
					t.Fatal(e)
				}
			}
			if admitted != 100 {
				t.Fatal("site pending budget changed", admitted)
			}
			if e = s.Admit(context.Background(), message.FetcherRequest{URI: "http://independent.onion/"}); e != nil {
				t.Fatal(e)
			}
			rows, e := s.Candidates(context.Background(), "host_diverse", "", 1000)
			if e != nil || len(rows) != 2 {
				t.Fatal("subdomains received extra site credit", len(rows), e)
			}
			counts, e := s.Counts()
			if e != nil || counts["pending_and_active"] != 101 {
				t.Fatal(counts, e)
			}
		})
	}
}
