package protection

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPersistenceFailureBlocksAdmissionUntilDurableRecovery(t *testing.T) {
	p := policy(t)
	// SQLite enforces this fault at the persistence boundary, without a mock store.
	if _, e := p.db.Exec(`CREATE TRIGGER fail_incident BEFORE INSERT ON metadata WHEN NEW.kind='incident' BEGIN SELECT RAISE(FAIL,'synthetic disk failure'); END`); e != nil {
		t.Fatal(e)
	}
	_, e := p.Trigger("http://blocked.example/", "synthetic", "crawl", "run", 0)
	if e == nil || errors.Is(e, ErrBlocked) {
		t.Fatalf("persistence failure hidden: %v", e)
	}
	if !errors.Is(p.Check("http://unrelated.test/"), ErrUnavailable) {
		t.Fatal("unrelated admission continued with an undurable deny")
	}
	if e := p.Guard("http://unrelated.test/", func() error { t.Fatal("write admitted"); return nil }); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- p.WaitHealthy(context.Background()) }()
	select {
	case <-done:
		t.Fatal("unhealthy wait released")
	case <-time.After(10 * time.Millisecond):
	}
	if _, e := p.db.Exec(`DROP TRIGGER fail_incident`); e != nil {
		t.Fatal(e)
	}
	if e := p.Flush(); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("recovery did not release waiters")
	}
	if !errors.Is(p.Check("http://blocked.example/"), ErrBlocked) || p.Check("http://unrelated.test/") != nil {
		t.Fatal("recovered policy incorrect")
	}
	var stored int
	if e := p.db.QueryRow(`SELECT count(*) FROM metadata WHERE kind='incident'`).Scan(&stored); e != nil || stored != 1 {
		t.Fatal(stored, e)
	}
}

func TestActivationCancelsWithoutWaitingForSlowWriteAndPurgeFencesIt(t *testing.T) {
	p := policy(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	written := make(chan error, 1)
	go func() {
		written <- p.Guard("http://blocked.test/", func() error { close(entered); <-release; return nil })
	}()
	<-entered
	ctx, done, e := p.Begin(context.Background(), "http://blocked.test/")
	if e != nil {
		t.Fatal(e)
	}
	defer done()
	activated := make(chan error, 1)
	go func() { _, e := p.Trigger("http://blocked.test/", "synthetic", "crawl", "run", 0); activated <- e }()
	select {
	case e := <-activated:
		if !errors.Is(e, ErrBlocked) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("slow sink prevented immediate deny")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("active request not cancelled")
	}
	drained := make(chan error, 1)
	go func() { drained <- p.WaitWrites(context.Background()) }()
	select {
	case <-drained:
		t.Fatal("purge passed an unfinished write")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	if e := <-written; e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-drained:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("write fence not released")
	}
	if e := p.Guard("http://blocked.test/", func() error { t.Fatal("late write admitted"); return nil }); !errors.Is(e, ErrBlocked) {
		t.Fatal(e)
	}
}
