package queue

import (
	"context"
	"github.com/iakinsey/delver/types"
)

type Queue interface {
	Start() error
	Stop() error
	GetChannel() chan types.Message
	Put(types.Message, int) error
	Prepare() error
	EndTransaction(types.Message, bool) error
	Len() int64
}

type OutputStore interface {
	LoadOutputs(types.Message) ([]types.Message, bool, error)
	SaveOutputs(types.Message, []types.Message) error
}

func PutContext(ctx context.Context, q Queue, msg types.Message, priority int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if contextual, ok := q.(interface {
		PutContext(context.Context, types.Message, int) error
	}); ok {
		return contextual.PutContext(ctx, msg, priority)
	}
	return q.Put(msg, priority)
}
