package queue

import (
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/types"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCandidateCachePreservesOrderAcrossLateEnqueue(t *testing.T) {
	root := t.TempDir()
	q := NewFileQueue(FileQueueParams{Name: "fixture", Path: root + "/pending", DlqPath: root + "/failed", MaxAttempts: 3}).(*fileQueue)
	payload, _ := json.Marshal(types.Message{ID: "fixture", Message: []byte(`{}`)})
	add := func(i int) {
		t.Helper()
		name := fmt.Sprintf("0-1700000000000000000-%d-fixture", i)
		if e := os.WriteFile(filepath.Join(q.path, name), payload, 0600); e != nil {
			t.Fatal(e)
		}
		q.putLock.Lock()
		q.notifyPendingLocked(name)
		q.putLock.Unlock()
	}
	for i := 0; i < 600; i++ {
		add(i)
	}
	for i := 0; i < 601; i++ {
		if i == 100 {
			add(9999)
		}
		m, e := q.next()
		if e != nil {
			t.Fatal(i, e)
		}
		want := i
		if i == 600 {
			want = 9999
		}
		if m.DeliveryID != fmt.Sprintf("0-1700000000000000000-%d-fixture", want) {
			t.Fatalf("at %d got %s", i, m.DeliveryID)
		}
		if e = q.EndTransaction(*m, true); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := q.next(); e != errQueueEmpty {
		t.Fatal(e)
	}
	if q.Performance()["cached_candidates"] > 256 {
		t.Fatal("unbounded cache")
	}
}
func TestCandidateCacheReconcilesExternalFilesAndRetry(t *testing.T) {
	root := t.TempDir()
	q := NewFileQueue(FileQueueParams{Name: "fixture", Path: root + "/pending", DlqPath: root + "/failed", MaxAttempts: 3}).(*fileQueue)
	if _, e := q.next(); e != errQueueEmpty {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(types.Message{ID: "fixture", Message: []byte(`{}`)})
	name := "0-1700000000000000000-1-fixture"
	if e := os.WriteFile(filepath.Join(q.path, name), raw, 0600); e != nil {
		t.Fatal(e)
	}
	q.candidatesUntil = time.Now().Add(-time.Second)
	m, e := q.next()
	if e != nil {
		t.Fatal(e)
	}
	if e = q.EndTransaction(*m, false); e != nil {
		t.Fatal(e)
	}
	m, e = q.next()
	if e != nil || m.Attempts != 1 {
		t.Fatal(m, e)
	}
	if e = q.EndTransaction(*m, true); e != nil {
		t.Fatal(e)
	}
}
