package worker

import (
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
	messages := s.inbox.GetChannel()

	for {
		select {
		case message := <-messages:
			s.worker.OnMessage(message)
		case <-s.terminate:
			s.terminated <- true
			return
		}
	}
}
