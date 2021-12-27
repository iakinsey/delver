package worker

import (
	"github.com/iakinsey/delver/types"
)

type Worker interface {
	OnMessage(types.Message) (interface{}, error)
	OnComplete()
}
