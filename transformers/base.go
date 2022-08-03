package transformers

import (
	"encoding/json"

	"github.com/iakinsey/delver/types"
	log "github.com/sirupsen/logrus"
)

type Transformer interface {
	Perform(msg json.RawMessage) (*types.Indexable, error)
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
