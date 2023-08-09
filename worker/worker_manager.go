package worker

import (
	"encoding/json"
	"reflect"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/armon/go-metrics"
	"github.com/iakinsey/delver/instrument"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
)

type WorkerManager interface {
	Start()
	Stop()
}
type workerManager struct {
	inbox       queue.Queue
	outbox      queue.Queue
	priority    int
	worker      Worker
	terminating bool
	terminate   chan bool
	terminated  chan bool
	metrics     metrics.MetricSink
	workerName  string
	termLock    sync.Mutex
}

func NewWorkerManager(worker Worker, inbox queue.Queue, outbox queue.Queue) WorkerManager {
	return &workerManager{
		inbox:       inbox,
		outbox:      outbox,
		priority:    0,
		worker:      worker,
		terminating: false,
		terminate:   make(chan bool),
		terminated:  make(chan bool),
		metrics:     instrument.GetMetrics(),
		workerName:  reflect.TypeOf(worker).Elem().Name(),
	}
}

func (s *workerManager) Start() {
	for {
		select {
		case message := <-s.inbox.GetChannel():
			if s.isTerminating() {
				s.metrics.IncrCounter([]string{s.workerName, "terminated"}, 1)
				s.terminated <- true
				return
			}

			s.metrics.IncrCounter([]string{s.workerName, "message", "in"}, 1)
			start := time.Now().UnixMilli()
			result, err := s.worker.OnMessage(message)
			end := time.Now().UnixMilli()
			success := err == nil

			s.metrics.AddSample([]string{s.workerName, "duration", "millisecond"}, float32(end-start))

			if success {
				s.metrics.IncrCounter([]string{s.workerName, "success"}, 1)
				s.publishResponse(result)
			} else {
				s.metrics.IncrCounter([]string{s.workerName, "error"}, 1)
				log.Errorf("Error occured while processing message: %s", err)
			}

			if err := s.inbox.EndTransaction(message, err == nil); err != nil {
				s.metrics.IncrCounter([]string{s.workerName, "inbox", "transaction", "error"}, 1)
				log.Error(err)
			} else {
				s.metrics.IncrCounter([]string{s.workerName, "inbox", "transaction", "success"}, 1)
			}
		case <-s.terminate:
			s.metrics.IncrCounter([]string{s.workerName, "terminated"}, 1)
			s.terminated <- true
			return
		}
	}
}

func (s *workerManager) Stop() {
	defer s.worker.OnComplete()

	s.setTerminating()
	s.terminate <- true
	<-s.terminated
}

func (s *workerManager) setTerminating() {
	s.termLock.Lock()
	defer s.termLock.Unlock()

	s.terminating = true
}

func (s *workerManager) isTerminating() bool {
	s.termLock.Lock()
	defer s.termLock.Unlock()

	return s.terminating
}

func (s *workerManager) publishResponse(result interface{}) {
	if result == nil {
		return
	}

	count := 1
	switch d := result.(type) {
	case types.MultiMessage:
		for _, r := range d.Values {
			count += 1
			s.doPublish(r)
		}
	default:
		s.doPublish(result)
	}

	s.metrics.IncrCounter([]string{s.workerName, "message", "out"}, float32(count))
}

func (s *workerManager) doPublish(result interface{}) {
	messageType, err := message.GetMessageTypeMapping(result)

	if err != nil {
		log.Errorf("%s: unknown message type attempted to publish: %s", s.workerName, err.Error())
		return
	}

	msg, err := json.Marshal(result)

	if err != nil {
		log.Errorf("%s: failed to serialize message segment: %s", s.workerName, err.Error())
		return
	}

	message := types.Message{}
	message.MessageType = messageType
	message.Message = json.RawMessage(msg)

	s.outbox.Put(message, s.priority)
}
