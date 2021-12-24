package worker

import (
	"github.com/iakinsey/delver/model"
)

type Worker interface {
	OnMessage(model.Message) error
	OnComplete()
}
