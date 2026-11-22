package report

import (
	"context"
	"fmt"
	"github.com/iakinsey/delver/site"
	"path/filepath"
	"testing"
)

func TestIncomingPurgeDepthAndCycles(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "report.sqlite"), false)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	edges := []URL{{"http://target.test/", "http://a.ref.test/"}, {"http://a.ref.test/", "http://next.test/"}, {"http://next.test/", "http://third.test/"}, {"http://a.ref.test/cycle", "http://target.test/"}, {"http://out.test/", "http://target.test/"}}
	if e = s.Discover(ctx, edges); e != nil {
		t.Fatal(e)
	}
	for d, want := range []int{1, 2, 3, 4} {
		v, _, _, e := s.IncomingSites(ctx, "target.test", d)
		if e != nil || len(v) != want {
			t.Fatalf("depth %d: %+v %v", d, v, e)
		}
		for _, x := range v {
			if x.Site == "out.test" {
				t.Fatal("followed outgoing edge")
			}
		}
	}
	if e = s.PurgeSite(ctx, "ref.test"); e != nil {
		t.Fatal(e)
	}
	v, _, _, e := s.IncomingSites(ctx, "target.test", 3)
	if e != nil || len(v) != 1 {
		t.Fatal(v, e)
	}
	counts, e := s.Counts(ctx)
	if e != nil || counts.Hosts != 4 {
		t.Fatal(counts, e)
	}
}

func TestCanonicalHostMigrationResumesFromDurableSeekCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration.sqlite")
	s, e := Open(path, false)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	for n := 1; n <= 1800; n++ {
		name := fmt.Sprintf("sub%d.site-%d.test", n, n/10)
		if _, e = tx.Exec("INSERT INTO hosts(id,name) VALUES(?,?)", n, name); e != nil {
			t.Fatal(e)
		}
		if n <= 512 {
			if _, e = tx.Exec("INSERT INTO site_hosts(host,site) VALUES(?,?)", n, site.Key(name)); e != nil {
				t.Fatal(e)
			}
		}
	}
	if _, e = tx.Exec("UPDATE progress SET value='512' WHERE key='site_hosts_migration'"); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(path, false)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var count int
	var marker string
	if e = s.db.QueryRow("SELECT count(*) FROM site_hosts").Scan(&count); e != nil {
		t.Fatal(e)
	}
	if count != 1800 {
		t.Fatalf("mapped %d", count)
	}
	if e = s.db.QueryRow("SELECT value FROM progress WHERE key='site_hosts_migration'").Scan(&marker); e != nil {
		t.Fatal(e)
	}
	if marker != "complete" {
		t.Fatal(marker)
	}
	if e = s.Discover(context.Background(), []URL{{URI: "http://new.example.test/page"}}); e != nil {
		t.Fatal(e)
	}
	var identity string
	if e = s.db.QueryRow("SELECT site FROM site_hosts JOIN hosts ON hosts.id=site_hosts.host WHERE hosts.name='new.example.test'").Scan(&identity); e != nil {
		t.Fatal(e)
	}
	if identity != "example.test" {
		t.Fatal(identity)
	}
}
