package publisher

import (
	"net/url"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/worker"
	log "github.com/sirupsen/logrus"
)

type fixedSeedPublisher struct {
	uris []string
}

type FixedSeedPublisherParams struct {
	Uris []string `json:"uris"`
}

func NewFixedSeedPublisher(params FixedSeedPublisherParams) worker.Worker {
	return &fixedSeedPublisher{
		uris: params.Uris,
	}
}

func (s *fixedSeedPublisher) OnMessage(msg types.Message) (interface{}, error) {
	var messages []interface{}

	for _, uri := range s.uris {
		meta, err := url.Parse(uri)

		if err != nil {
			log.Errorf("failed to parse url: %s", uri)
			continue
		}

		messages = append(messages, message.FetcherRequest{
			RequestID: types.NewV4(),
			URI:       uri,
			Host:      meta.Host,
			Protocol:  types.ProtocolHTTP,
			Depth:     1,
		})
	}

	return types.MultiMessage{
		Values: messages,
	}, nil
}

func (s *fixedSeedPublisher) OnComplete() {}
