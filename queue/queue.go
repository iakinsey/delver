package queue

import "github.com/iakinsey/delver/model"

type Queue interface {
	GetChannel() chan model.Message
	Put(model.Message) error
}
