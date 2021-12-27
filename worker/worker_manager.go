package worker

import (
	"encoding/json"
	"log"

	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
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
}

func NewWorkerManager(worker Worker, inbox queue.Queue, outbox queue.Queue) WorkerManager {
	return &workerManager{
		inbox:      inbox,
		outbox:     outbox,
		priority:   0,
		worker:     worker,
		terminate:  make(chan bool),
		terminated: make(chan bool),
	}
}

func (s *workerManager) Start() {
	for {
		select {
		case message := <-s.inbox.GetChannel():
			result, err := s.worker.OnMessage(message)
			success := err == nil

			if success {
				s.publishResponse(result)
			} else {
				log.Printf("Error occured while processing message: %s", err)
			}

			if err := s.inbox.EndTransaction(message, err == nil); err != nil {
				log.Print(err.Error())
			}
		case <-s.terminate:
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

	message := types.Message{}
	message.MessageType = messageType
	message.Message = json.RawMessage(msg)

	s.outbox.Put(message, s.priority)
}
