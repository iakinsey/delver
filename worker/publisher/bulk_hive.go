package publisher

import (
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/worker"
)

type bulkHivePublisher struct {
}

func NewBulkHivePublisher() worker.Worker {
	return &bulkHivePublisher{}
}

func (s *bulkHivePublisher) OnMessage(msg types.Message) (interface{}, error) {
	return nil, nil
}

func (s *bulkHivePublisher) OnComplete() {}
