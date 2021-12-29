package extractor

import (
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/worker"
)

type compositeExtractor struct {
}

func NewCompositeExtractorWorker() worker.Worker {
	return &compositeExtractor{}
}

func (s *compositeExtractor) OnMessage(msg types.Message) (interface{}, error) {
	return nil, nil
}

func (s *compositeExtractor) OnComplete() {}
