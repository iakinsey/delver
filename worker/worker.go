package worker

import (
	"context"
	"github.com/iakinsey/delver/types"
)

type Worker interface {
	OnMessage(types.Message) (interface{}, error)
	OnComplete()
}

type MessageCommitter interface {
	OnCommit(types.Message) error
}

type ContextWorker interface {
	OnMessageContext(context.Context, types.Message) (interface{}, error)
}
