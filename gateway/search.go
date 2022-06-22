package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elastic/go-elasticsearch/esapi"
	"github.com/elastic/go-elasticsearch/v7"
	"github.com/elastic/go-elasticsearch/v7/esutil"
	"github.com/iakinsey/delver/types"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
)

const indexExist = "resource_already_exists_exception"

type searchGateway struct {
	client       *elasticsearch.Client
	bulkIndexers map[string]esutil.BulkIndexer
}

type SearchGateway interface {
	Index(*types.Indexable) error
	IndexMany([]*types.Indexable) error
	Declare(types.Index)
}

func NewSearchGateway(addresses []string) SearchGateway {
	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: addresses,
	})

	if err != nil {
		logrus.Panicf("failed to create search gateway: %s", err)
	}

	return &searchGateway{
		client: client,
	}
}

func (s *searchGateway) Index(indexable *types.Indexable) error {
	payload, err := json.Marshal(indexable.Data)

	if err != nil {
		return errors.Wrap(err, "failed to serialize resource for logging")
	}

	res, err := s.client.Index(
		indexable.Index,
		bytes.NewReader(payload),
		s.client.Index.WithDocumentID(indexable.ID),
	)

	if err != nil {
		return errors.Wrap(err, "failed to index entity")
	} else if res.StatusCode >= 300 {
		return fmt.Errorf("failed to index entity (code %d): %s", res.StatusCode, res.String())
	}

	return nil

}

func (s *searchGateway) Declare(index types.Index) {
	request := esapi.IndicesCreateRequest{
		Index:  index.Name,
		Human:  true,
		Pretty: true,
		Body:   strings.NewReader(index.Spec),
	}

	if res, err := request.Do(context.Background(), s.client); err != nil {
		log.Panicf("failed to create index: %s", err)
	} else if res.StatusCode == 400 && strings.Contains(res.String(), indexExist) {
		return
	} else if res.StatusCode >= 300 {
		log.Panicf("failed to create index (code %d): %s", res.StatusCode, res.String())
	}

	return
}

func (s *searchGateway) IndexMany(entities []*types.Indexable) error {
	for _, indexable := range entities {
		indexer, err := s.getOrCreateBulkIndexer(indexable.Index)

		if err != nil {
			return errors.Wrap(err, "IndexMany get bulk indexer")
		}

		data, err := json.Marshal(indexable.Data)

		if err != nil {
			return errors.Wrap(err, "IndexMany serialize json")
		}

		indexer.Add(
			context.Background(),
			esutil.BulkIndexerItem{
				Action:     "index",
				DocumentID: indexable.ID,
				Body:       bytes.NewReader(data),
				OnFailure:  onBulkFailure,
			},
		)
	}

	return nil
}

func (s *searchGateway) getOrCreateBulkIndexer(index string) (esutil.BulkIndexer, error) {
	if indexer, ok := s.bulkIndexers[index]; ok {
		return indexer, nil
	}

	indexer, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{
		Index:  index,
		Client: s.client,
	})

	if err != nil {
		return nil, errors.Wrap(err, "getOrCreateBulkIndexer")
	}

	s.bulkIndexers[index] = indexer

	return indexer, nil
}

func onBulkFailure(ctx context.Context, bii esutil.BulkIndexerItem, biri esutil.BulkIndexerResponseItem, err error) {
	logrus.Errorf("failed to index entity %s: %s", bii.DocumentID, err)
}
