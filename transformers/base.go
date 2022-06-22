package transformers

import (
	"encoding/json"

	"github.com/iakinsey/delver/types"
	log "github.com/sirupsen/logrus"
)

const (
	Article = "article"
	Metric  = "metric"
	Page    = "page"
)

type Transformer interface {
	Perform(msg json.RawMessage) (*types.Indexable, error)
	Input() types.MessageType
	Streamable() bool
}

func GetTransformer(name string) Transformer {
	switch name {
	case Article:
		return NewArticleTransformer()
	case Metric:
		return NewMetricTransformer()
	case Page:
		return NewPageTransformer()
	default:
		log.Panicf("unknown transformer %s", name)
	}

	return nil
}
