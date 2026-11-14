package queue

import "github.com/iakinsey/delver/types"

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
