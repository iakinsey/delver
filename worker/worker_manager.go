package worker

import "github.com/iakinsey/delver/queue"

type WorkerManager interface {
	Start() error
	Stop() error
}

type workerManager struct {
	inbox         queue.Queue
	outbox        queue.Queue
	maxCoroutines int32
}

func NewWorkerManager(inbox queue.Queue, outbox queue.Queue, maxCoroutines int32) WorkerManager {
	return &workerManager{}
}

func (s *workerManager) Start() error {
	return nil
}

func (s *workerManager) Stop() error {
	return nil
}
