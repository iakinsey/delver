package transformer

import (
	"context"
	"github.com/hashicorp/go-multierror"
	"github.com/iakinsey/delver/gateway"
	"github.com/iakinsey/delver/transformers"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/worker"
	log "github.com/sirupsen/logrus"
)

type transformer struct {
	crawlID, runID string
	t              []transformers.Transformer
	search         gateway.SearchGateway
}

type TransformerParams struct {
	CrawlID         string   `json:"-"`
	RunID           string   `json:"-"`
	Enabled         []string `json:"enabled"`
	SearchAddresses []string `json:"search_addresses"`
}

func NewTransformerWorker(opts TransformerParams) worker.Worker {
	return NewTransformerWorkerContext(context.Background(), opts)
}
func NewTransformerWorkerContext(ctx context.Context, opts TransformerParams) worker.Worker {
	var t []transformers.Transformer

	for _, name := range opts.Enabled {
		t = append(t, transformers.GetTransformer(name))
	}

	search := gateway.NewSearchGateway(opts.SearchAddresses)

	for _, index := range types.Indices {
		if contextual, ok := search.(interface {
			DeclareContext(context.Context, types.Index) error
		}); ok {
			if err := contextual.DeclareContext(ctx, index); err != nil {
				panic(err)
			}
		} else {
			search.Declare(index)
		}
	}

	return &transformer{
		crawlID: opts.CrawlID, runID: opts.RunID,
		t:      t,
		search: search,
	}
}

func (s *transformer) OnMessage(msg types.Message) (interface{}, error) {
	return s.OnMessageContext(context.Background(), msg)
}
func (s *transformer) OnMessageContext(ctx context.Context, msg types.Message) (interface{}, error) {
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

		entities = append(entities, idx...)
	}

	for _, entity := range entities {
		entity.CrawlID, entity.RunID = s.crawlID, s.runID
	}

	var indexErr error
	if contextual, ok := s.search.(interface {
		IndexManyContext(context.Context, []*types.Indexable) error
	}); ok {
		indexErr = contextual.IndexManyContext(ctx, entities)
	} else {
		indexErr = s.search.IndexMany(entities)
	}
	if err := indexErr; err != nil {
		tErr = multierror.Append(tErr, err)
	} else {
		log.Infof("transformer indexed %d entities", len(entities))
	}

	return nil, tErr
}

func (s *transformer) OnComplete() {}
