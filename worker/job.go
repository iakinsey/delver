package worker

import (
	"github.com/armon/go-metrics"
	"sync"
	"time"

	"github.com/iakinsey/delver/queue"
)

type jobManager struct {
	startOnce sync.Once
	worker    Worker
	timer     queue.Queue
	outbox    queue.Queue
	delay     time.Duration
	manager   WorkerManager
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
	s.startOnce.Do(func() { s.timer.Start() })
	s.manager.Start()
}

func (s *jobManager) Stop() {
	s.timer.Stop()
	s.manager.Stop()
}

func (s *jobManager) Pause(value bool) { s.manager.(interface{ Pause(bool) }).Pause(value) }
func (s *jobManager) Activity() Activity {
	return s.manager.(interface{ Activity() Activity }).Activity()
}

func (s *jobManager) SetMetricSink(sink metrics.MetricSink) {
	s.manager.(interface{ SetMetricSink(metrics.MetricSink) }).SetMetricSink(sink)
}

func (s *jobManager) Health() string {
	if source, ok := s.worker.(interface{ Health() string }); ok {
		return source.Health()
	}
	return ""
}
