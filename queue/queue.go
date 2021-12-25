package queue

import "github.com/iakinsey/delver/model"

type Queue interface {
	Start() error
	Stop() error
	GetChannel() chan model.Message
	Put(model.Message, int) error
	Prepare() error
	EndTransaction(model.Message, bool) error
}
