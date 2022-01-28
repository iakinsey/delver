package publisher

import (
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/worker"
)

type rssFeedPublisher struct{}

func NewRssFeedPublisher(uris []string) worker.Worker {
	return &rssFeedPublisher{}
}

func (s *rssFeedPublisher) OnMessage(msg types.Message) (interface{}, error) {
	return nil, nil
}

func (s *rssFeedPublisher) OnComplete() {}
