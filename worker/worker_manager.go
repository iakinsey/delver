package worker

import (
	"fmt"
	"log"
	"time"

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
	worker        Worker
	terminate     chan bool
	terminated    chan bool
}

func NewWorkerManager(worker Worker, inbox queue.Queue, outbox queue.Queue, maxCoroutines int) WorkerManager {
	return &workerManager{
		inbox:         inbox,
		outbox:        outbox,
		maxCoroutines: maxCoroutines,
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
			err := s.worker.OnMessage(message)
			success := err == nil

			if !success {
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
