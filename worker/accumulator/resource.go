package accumulator

import (
	"encoding/json"

	"github.com/hashicorp/go-multierror"
	"github.com/iakinsey/delver/gateway/logger"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/worker"
)

type resourceAccumulator struct {
	loggers []logger.Logger
}

func NewResourceAccumulator(loggers []logger.Logger) worker.Worker {
	return &resourceAccumulator{
		loggers: loggers,
	}
}

func (s *resourceAccumulator) OnMessage(msg types.Message) (interface{}, error) {
	composite := message.CompositeAnalysis{}

	if err := json.Unmarshal(msg.Message, &composite); err != nil {
		return nil, err
	}

	var loggerErr error
	result := make(chan error, len(s.loggers))

	for _, l := range s.loggers {
		go func(l logger.Logger) {
			result <- l.LogResource(composite)
		}(l)
	}

	for i := 0; i < len(s.loggers); i++ {
		if err := <-result; err != nil {
			loggerErr = multierror.Append(loggerErr, err)
		}
	}

	return nil, loggerErr
}

func (s *resourceAccumulator) OnComplete() {}
