package worker

import (
	"time"

	"github.com/iakinsey/delver/queue"
)

type jobManager struct {
	worker  Worker
	outbox  queue.Queue
	delay   time.Duration
	manager WorkerManager
}

func NewJobManager(worker Worker, outbox queue.Queue, delay time.Duration) WorkerManager {
	inbox := queue.NewTimerQueue(delay)
	manager := NewWorkerManager(worker, inbox, outbox)

	return &jobManager{
		worker:  worker,
		outbox:  outbox,
		delay:   delay,
		manager: manager,
	}
}

func (s *jobManager) Start() {
	s.manager.Start()
}

func (s *jobManager) Stop() {
	s.manager.Stop()
}
