package logger

import (
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/worker"
)

/*
type Worker interface {
	OnMessage(types.Message) (interface{}, error)
	OnComplete()
}
*/

type websiteLogger struct {
}

func NewWebsiteLogger() worker.Worker {
	return &websiteLogger{}
}

func (s *websiteLogger) OnMessage(msg types.Message) (interface{}, error) {
	return nil, nil
}

func (s *websiteLogger) OnComplete() {}
