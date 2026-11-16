package publisher

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/iakinsey/delver/resource/maps"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type auditPublisherQueue struct {
	puts      int
	failAfter int
}

func (q *auditPublisherQueue) Start() error                   { return nil }
func (q *auditPublisherQueue) Stop() error                    { return nil }
func (q *auditPublisherQueue) GetChannel() chan types.Message { return nil }
func (q *auditPublisherQueue) Put(types.Message, int) error {
	q.puts++
	if q.puts > q.failAfter {
		return errors.New("disk full")
	}
	return nil
}
func (q *auditPublisherQueue) Prepare() error                           { return nil }
func (q *auditPublisherQueue) EndTransaction(types.Message, bool) error { return nil }
func (q *auditPublisherQueue) Len() int64                               { return 0 }
func TestRegressionPublisherLockOnError(t *testing.T) {
	s := NewDfsBasicPublisher(DfsBasicPublisherParams{OutputQueue: &auditPublisherQueue{}, UrlStorePath: filepath.Join(t.TempDir(), "missing")})
	if _, err := s.OnMessage(types.Message{}); err == nil {
		t.Fatal("expected missing folder error")
	}
	done := make(chan struct{})
	go func() { s.OnMessage(types.Message{}); close(done) }()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("second OnMessage deadlocks after first fillQueue error")
	}
}
func TestRegressionPublisherPartialFailurePreservesURLs(t *testing.T) {
	root := t.TempDir()
	host := "example.test"
	hostPath := filepath.Join(root, base64.URLEncoding.EncodeToString([]byte(host)))
	urls := maps.NewPersistentMap(maps.PersistentMapParams{Path: hostPath})
	for _, uri := range []string{"https://example.test/a", "https://example.test/b"} {
		b, _ := json.Marshal(message.FetcherRequest{URI: uri})
		urls.Set([]byte(uri), b)
	}
	urls.Close()
	visited := maps.NewPersistentMap(maps.PersistentMapParams{Path: filepath.Join(t.TempDir(), "visited")})
	defer visited.Close()
	q := &auditPublisherQueue{failAfter: 1}
	s := NewDfsBasicPublisher(DfsBasicPublisherParams{OutputQueue: q, UrlStorePath: root, VisitedHosts: visited}).(*dfsBasicPublisher)
	s.robots = nil
	_, err := s.OnMessage(types.Message{})
	if err == nil {
		t.Error("partial queue failure reported as success")
	}
	if _, err := os.Stat(hostPath); err != nil {
		t.Error("host URL database deleted after only one of two URLs was accepted")
	}

}
