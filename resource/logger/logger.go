package logger

import "github.com/iakinsey/delver/types/message"

type Logger interface {
	LogResource(message.CompositeAnalysis) error
}
