package transformer

import (
	"github.com/hashicorp/go-multierror"
	"github.com/iakinsey/delver/gateway"
	"github.com/iakinsey/delver/transformers"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/worker"
)

type transformer struct {
	t        []transformers.Transformer
	search   gateway.SearchGateway
	streamer gateway.ClientStreamer
}

type TransformerParams struct {
	Enabled         []string `json:"enabled"`
	SearchAddresses []string `json:"search_addresses"`
}

func NewTransformerWorker(opts TransformerParams) worker.Worker {
	var t []transformers.Transformer

	for _, name := range opts.Enabled {
		t = append(t, transformers.GetTransformer(name))
	}

	search := gateway.NewSearchGateway(opts.SearchAddresses)

	for _, index := range types.Indices {
		search.Declare(index)
	}

	return &transformer{
		t:        t,
		search:   search,
		streamer: gateway.NewClientStreamer(),
	}
}

func (s *transformer) OnMessage(msg types.Message) (interface{}, error) {
	var tErr error
	var entities []*types.Indexable

	for _, transformer := range s.t {
		if transformer.Input() != msg.MessageType {
			continue
		}

		idx, err := transformer.Perform(msg.Message)

		if err != nil {
			tErr = multierror.Append(tErr, err)
		}

		entities = append(entities, idx)
	}

	if err := s.search.IndexMany(entities); err != nil {
		tErr = multierror.Append(tErr, err)
	}

	if err := s.streamer.Publish(entities); err != nil {
		tErr = multierror.Append(tErr, err)
	}

	return nil, tErr
}

func (s *transformer) OnComplete() {}
