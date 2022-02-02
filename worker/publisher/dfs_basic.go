package publisher

import (
	"sync"
	"time"

	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/util/maps"
	"github.com/iakinsey/delver/worker"
)

type dfsBasicPublisher struct {
	inputQueue         queue.Queue
	urlStorePath       string
	visitedDomainsPath string
	urlStore           maps.Map
	visitedDomains     maps.Map
	rotateAfter        time.Duration
	timeSinceEmpty     *time.Time
	lock               sync.Mutex
}

func NewDfsBasicPublisher(inputQueue queue.Queue, urlStorePath string, visitedDomainsPath string, rotateAfter time.Duration) worker.Worker {
	return &dfsBasicPublisher{
		inputQueue:         inputQueue,
		urlStorePath:       urlStorePath,
		visitedDomainsPath: visitedDomainsPath,
		urlStore:           maps.NewMultiHostMap(urlStorePath),
		visitedDomains:     maps.NewPersistentMap(visitedDomainsPath),
		rotateAfter:        rotateAfter,
		lock:               sync.Mutex{},
	}
}

func (s *dfsBasicPublisher) OnMessage(msg types.Message) (interface{}, error) {
	s.lock.Lock()

	now := time.Now()
	queueEmpty := s.inputQueue.Len() == 0


	if queueEmpty && s.timeSinceEmpty != nil && s.timeSinceEmpty.Add(s.rotateAfter).Before(now) {
		// Fill the queue
		s.fillQueue()
		s.timeSinceEmpty = nil
	} else if queueEmpty && s.timeSinceEmpty == nil {
		s.timeSinceEmpty = &now
	} else if !queueEmpty {
		s.timeSinceEmpty = nil
	}

	s.lock.Unlock()

	return nil, nil
}

func (s *dfsBasicPublisher) fillQueue() {

}

func (s *dfsBasicPublisher) OnComplete() {}
