package worker

import (
	"errors"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"testing"
	"time"
)

type auditQueue struct {
	ch     chan types.Message
	ack    chan bool
	putErr error
}

func (q *auditQueue) Start() error                                  { return nil }
func (q *auditQueue) Stop() error                                   { return nil }
func (q *auditQueue) GetChannel() chan types.Message                { return q.ch }
func (q *auditQueue) Put(types.Message, int) error                  { return q.putErr }
func (q *auditQueue) Prepare() error                                { return nil }
func (q *auditQueue) EndTransaction(_ types.Message, ok bool) error { q.ack <- ok; return nil }
func (q *auditQueue) Len() int64                                    { return 0 }

type auditWorker struct{}

func (*auditWorker) OnMessage(types.Message) (interface{}, error) {
	return message.FetcherRequest{URI: "https://example.test"}, nil
}
func (*auditWorker) OnComplete() {}
func TestRegressionPublishFailureAcknowledgment(t *testing.T) {
	inbox := &auditQueue{ch: make(chan types.Message, 1), ack: make(chan bool, 1)}
	outbox := &auditQueue{putErr: errors.New("disk full")}
	m := NewWorkerManager(&auditWorker{}, inbox, outbox)
	go m.Start()
	defer m.Stop()
	inbox.ch <- types.Message{}
	select {
	case success := <-inbox.ack:
		if success {
			t.Fatal("input acknowledged successfully after outbox returned disk full")
		}
	case <-time.After(time.Second):
		t.Fatal("no acknowledgment")
	}
}
