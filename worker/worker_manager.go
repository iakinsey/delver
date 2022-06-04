package worker

import (
	"encoding/json"
	"reflect"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/armon/go-metrics"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
)

type WorkerManager interface {
	Start()
	Stop()
}
type workerManager struct {
	inbox      queue.Queue
	outbox     queue.Queue
	priority   int
	worker     Worker
	terminate  chan bool
	terminated chan bool
	metrics    metrics.MetricSink
	workerName string
}

func NewWorkerManager(worker Worker, inbox queue.Queue, outbox queue.Queue) WorkerManager {
	return &workerManager{
		inbox:      inbox,
		outbox:     outbox,
		priority:   0,
		worker:     worker,
		terminate:  make(chan bool),
		terminated: make(chan bool),
		metrics:    util.GetMetrics(),
		workerName: reflect.TypeOf(worker).Elem().Name(),
	}
}

func (s *workerManager) Start() {
	for {
		select {
		case message := <-s.inbox.GetChannel():
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

	s.terminate <- true
	<-s.terminated
}

func (s *workerManager) publishResponse(result interface{}) {
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
		log.Errorln("Unknown message type attempted to publish")
		return
	}

	msg, err := json.Marshal(result)

	if err != nil {
		log.Errorln("Failed to serialize message segment")
		return
	}

	message := types.Message{}
	message.MessageType = messageType
	message.Message = json.RawMessage(msg)

	s.outbox.Put(message, s.priority)
}
