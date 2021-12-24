package queue

import "github.com/iakinsey/delver/model"

type Queue interface {
	Get() (*model.Message, error)
	Put(model.Message) error
}
