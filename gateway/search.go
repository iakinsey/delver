package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	"github.com/elastic/go-elasticsearch/v7"
	"github.com/elastic/go-elasticsearch/v7/esapi"
	"github.com/elastic/go-elasticsearch/v7/esutil"
	"github.com/iakinsey/delver/types"
	"github.com/pkg/errors"
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
	Search(query io.Reader) (entities []json.RawMessage, err error)
	SearchAggregate(query io.Reader) (entities []json.RawMessage, err error)
}

type hitsEntity struct {
	Source json.RawMessage `json:"_source"`
}

type searchResultHits struct {
	Hits []hitsEntity `json:"hits"`
}

type searchResult struct {
	Hits searchResultHits `json:"hits"`
}

type aggSearchResult struct {
	Aggregations map[string]json.RawMessage `json:"aggregations"`
}

func NewSearchGateway(addresses []string) SearchGateway {
	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: addresses,
	})

	if err != nil {
		log.Panicf("failed to create search gateway: %s", err)
	}

	return &searchGateway{
		client: client,
	}
}

func (s *searchGateway) Search(query io.Reader) (entities []json.RawMessage, err error) {
	data, err := s.doSearch(query)

	if err != nil {
		return nil, errors.Wrap(err, "failed to perform search")
	}

	result := searchResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, errors.Wrap(err, "failed to parse search output")
	}

	for _, hit := range result.Hits.Hits {
		entities = append(entities, hit.Source)
	}

	return
}

func (s *searchGateway) SearchAggregate(query io.Reader) (entities []json.RawMessage, err error) {
	data, err := s.doSearch(query)

	if err != nil {
		return nil, errors.Wrap(err, "failed to perform aggregate search")
	}

	result := aggSearchResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, errors.Wrap(err, "failed to parse aggregate search output")
	}

	if len(result.Aggregations) != 1 {
		return nil, errors.Wrapf(err, "too many aggregations, have: %d, want: 1", len(result.Aggregations))
	}

	key := ""

	for k := range result.Aggregations {
		key = k
		break
	}

	var bucketMap map[string][]json.RawMessage

	if err = json.Unmarshal(result.Aggregations[key], &bucketMap); err != nil {
		return nil, errors.Wrap(err, "failed to parse bucket map")
	}

	if _, ok := bucketMap["buckets"]; !ok {
		return nil, errors.New("search result does not contain aggregate buckets")
	}

	return bucketMap["buckets"], nil
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

func (s *searchGateway) doSearch(query io.Reader) ([]byte, error) {
	res, err := s.client.Search(
		s.client.Search.WithBody(query),
	)

	if err != nil {
		return nil, errors.Wrap(err, "failed to search entity")
	} else if res.StatusCode >= 300 {
		return nil, fmt.Errorf("failed to search entity (code %d): %s", res.StatusCode, res.String())
	}

	data, err := ioutil.ReadAll(res.Body)

	if err != nil {
		return nil, errors.Wrap(err, "failed to read search output")
	}

	return data, err
}

func onBulkFailure(ctx context.Context, bii esutil.BulkIndexerItem, biri esutil.BulkIndexerResponseItem, err error) {
	log.Errorf("failed to index entity %s: %s", bii.DocumentID, err)
}
