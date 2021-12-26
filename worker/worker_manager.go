package worker

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/iakinsey/delver/model"
	"github.com/iakinsey/delver/model/message"
	"github.com/iakinsey/delver/queue"
)

type WorkerManager interface {
	Start()
	Stop()
}
type workerManager struct {
	inbox         queue.Queue
	outbox        queue.Queue
	maxCoroutines int
	priority      int
	worker        Worker
	terminate     chan bool
	terminated    chan bool
}

func NewWorkerManager(worker Worker, inbox queue.Queue, outbox queue.Queue, maxCoroutines int) WorkerManager {
	return &workerManager{
		inbox:         inbox,
		outbox:        outbox,
		maxCoroutines: maxCoroutines,
		priority:      0,
		worker:        worker,
		terminate:     make(chan bool, maxCoroutines),
		terminated:    make(chan bool, maxCoroutines),
	}
}

func (s *workerManager) Start() {
	for i := 0; i < s.maxCoroutines; i = i + 1 {
		go s.doWork()
	}
}

func (s *workerManager) Stop() {
	defer s.worker.OnComplete()

	for i := 0; i < s.maxCoroutines; i = i + 1 {
		s.terminate <- true
	}

	for i := 0; i < s.maxCoroutines; i = i + 1 {
		select {
		case <-s.terminated:
			continue
		case <-time.After(10 * time.Millisecond):
			continue
		}
	}
}

func (s *workerManager) doWork() {
	for {
		select {
		case message := <-s.inbox.GetChannel():
			result, err := s.worker.OnMessage(message)
			success := err == nil

			if success {
				s.publishResponse(result)
			} else {
				log.Printf(fmt.Sprintf("Error occured while processing message: %s", err))
			}

			if err := s.inbox.EndTransaction(message, err == nil); err != nil {
				log.Printf(err.Error())
			}
		case <-s.terminate:
			s.terminated <- true
			return
		}
	}
}

func (s *workerManager) publishResponse(result interface{}) {
	messageType, err := message.GetMessageTypeMapping(result)

	if err != nil {
		log.Printf("Unknown message type attempted to publish")
		return
	}

	msg, err := json.Marshal(result)

	if err != nil {
		log.Printf("Failed to serialize message segment")
		return
	}

	message := model.Message{}
	message.MessageType = messageType
	message.Message = json.RawMessage(msg)

	s.outbox.Put(message, s.priority)
}
