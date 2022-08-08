package transformers

import (
	"encoding/json"

	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type Transformer interface {
	Perform(msg json.RawMessage) ([]*types.Indexable, error)
	Input() types.MessageType
	Streamable() bool
}

func GetTransformer(name string) Transformer {
	switch name {
	case types.ArticleIndexable:
		return NewArticleTransformer()
	case types.MetricIndexable:
		return NewMetricTransformer()
	case types.PageIndexable:
		return NewPageTransformer()
	default:
		log.Panicf("unknown transformer: %s", name)
	}

	return nil
}

func Send(q queue.Queue, id string, msgType types.MessageType, data interface{}) error {
	if q == nil {
		return nil
	}

	b, err := json.Marshal(data)

	if err != nil {
		return errors.Wrap(err, "failed to serialize transformer message")
	}

	if id == "" {
		id = string(types.NewV4())
	}

	transformerMsg := types.Message{
		ID:          id,
		MessageType: msgType,
		Message:     json.RawMessage(b),
	}

	return errors.Wrap(
		q.Put(transformerMsg, 0),
		"failed to send transformer message",
	)

}
