package logger

import "github.com/iakinsey/delver/types/message"

type zmqLogger struct{}

func NewZmqLogger() Logger {
	return &zmqLogger{}
}

func (s *zmqLogger) LogResource(c message.CompositeAnalysis) error {
	return nil
}
