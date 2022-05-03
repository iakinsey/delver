package worker

import (
	"time"

	"github.com/iakinsey/delver/queue"
)

type jobManager struct {
	worker  Worker
	timer   queue.Queue
	outbox  queue.Queue
	delay   time.Duration
	manager WorkerManager
}

func NewJobManager(worker Worker, outbox queue.Queue, delay time.Duration) WorkerManager {
	timer := queue.NewTimerQueue(queue.TimerQueueParams{Delay: delay})
	manager := NewWorkerManager(worker, timer, outbox)

	return &jobManager{
		worker:  worker,
		timer:   timer,
		outbox:  outbox,
		delay:   delay,
		manager: manager,
	}
}

func (s *jobManager) Start() {
	s.timer.Start()
	s.manager.Start()
}

func (s *jobManager) Stop() {
	s.manager.Stop()
}
