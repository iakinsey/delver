package report

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestWaitingWritesCommitBeforeAcknowledgmentAndIsolateErrors(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "report.db"), false)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.write.Lock()
	var wg sync.WaitGroup
	done := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			done <- s.Response(context.Background(), fmt.Sprintf("http://fixture.invalid/%d", i), 200)
		}(i)
	}
	deadline := time.Now().Add(time.Second)
	for {
		s.writer.waiting.Lock()
		n := len(s.writer.waiting.slots)
		s.writer.waiting.Unlock()
		if n == 32 {
			break
		}
		if time.Now().After(deadline) {
			s.write.Unlock()
			t.Fatal("writers did not queue")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case e := <-done:
		s.write.Unlock()
		t.Fatal("acknowledged uncommitted write", e)
	default:
	}
	s.write.Unlock()
	wg.Wait()
	close(done)
	for e := range done {
		if e != nil {
			t.Fatal(e)
		}
	}
	c, e := s.Counts(context.Background())
	if e != nil || c.Visited != 32 {
		t.Fatal(c, e)
	}
	sentinel := errors.New("controlled statement failure")
	if e = s.coalesced(context.Background(), func(tx *sql.Tx) error {
		_, e := recordURL(context.Background(), tx, "http://fixture.invalid/rollback", true)
		if e != nil {
			return e
		}
		return sentinel
	}); !errors.Is(e, sentinel) {
		t.Fatal(e)
	}
	c, _ = s.Counts(context.Background())
	if c.Visited != 32 {
		t.Fatal("partial transaction committed", c)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if e = s.Response(cancelled, "http://fixture.invalid/cancelled", 200); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	c, _ = s.Counts(context.Background())
	if c.Visited != 32 {
		t.Fatal(c)
	}
}
