package queue

import (
	"encoding/json"
	"github.com/iakinsey/delver/types"
	"os"
	"sync"
	"testing"
	"time"
)

func testFileQueue(t *testing.T) *fileQueue {
	t.Helper()
	return NewFileQueue(FileQueueParams{Name: "regression", Path: t.TempDir(), DlqPath: t.TempDir(), MaxPollDelayMs: 1, Reset: true, Resilient: true}).(*fileQueue)
}
func TestConcurrentPublication(t *testing.T) {
	q := testFileQueue(t)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := q.Put(types.Message{Message: json.RawMessage(`{}`)}, 0); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if q.Len() != 100 {
		t.Fatalf("lost messages: %d", q.Len())
	}
}
func TestRecoveryAndStop(t *testing.T) {
	q := testFileQueue(t)
	q.Put(types.Message{Message: json.RawMessage(`{}`)}, 0)
	if _, err := q.next(); err != nil {
		t.Fatal(err)
	}
	if err := q.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-q.GetChannel():
		if err := q.EndTransaction(msg, true); err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("claimed message not recovered")
	}
	done := make(chan struct{})
	go func() { q.Stop(); q.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stop blocked")
	}
}
func TestStopWithoutConsumer(t *testing.T) {
	for _, q := range []Queue{NewTimerQueue(TimerQueueParams{Delay: time.Hour}), testFileQueue(t)} {
		q.Start()
		done := make(chan struct{})
		go func(q Queue) { q.Stop(); close(done) }(q)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("stop blocked")
		}
	}
}
func TestNumericPriority(t *testing.T) {
	q := testFileQueue(t)
	q.Put(types.Message{Message: json.RawMessage(`2`)}, 2)
	q.Put(types.Message{Message: json.RawMessage(`10`)}, 10)
	m, err := q.next()
	if err != nil || string(m.Message) != "2" {
		t.Fatalf("wrong priority: %+v %v", m, err)
	}
}
func TestCorruptRecordQuarantined(t *testing.T) {
	q := testFileQueue(t)
	if err := os.WriteFile(q.path+"/0-1-1-regression", []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := q.next(); err == nil {
		t.Fatal("accepted corrupt record")
	}
	files, _ := os.ReadDir(q.dlqPath)
	if len(files) != 1 {
		t.Fatal("record not quarantined")
	}
}

func TestDeadLettersRetainBodyReferences(t *testing.T) {
	q := testFileQueue(t)
	q.maxAttempts = 1
	body := types.NewV4()
	payload, _ := json.Marshal(map[string]interface{}{"store_key": body})
	q.Put(types.Message{MessageType: types.FetcherResponseType, Message: payload}, 0)
	input, err := q.next()
	if err != nil {
		t.Fatal(err)
	}
	if err = q.EndTransaction(*input, false); err != nil {
		t.Fatal(err)
	}
	references, err := q.ReferencedObjects()
	if err != nil || len(references) != 1 || references[0] != body {
		t.Fatalf("dead-letter body reference lost: %v %v", references, err)
	}
}
