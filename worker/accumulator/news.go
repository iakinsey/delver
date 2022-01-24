package accumulator

import (
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/worker"
)

type newsAccumulator struct {
}

func NewNewsAccumulator() worker.Worker {
	return &newsAccumulator{}
}

func (s *newsAccumulator) OnMessage(msg types.Message) (interface{}, error) {
	return nil, nil
}

func (s *newsAccumulator) OnComplete() {}
