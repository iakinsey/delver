package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v7"
	"github.com/elastic/go-elasticsearch/v7/esapi"
	"github.com/iakinsey/delver/types"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const indexExist = "resource_already_exists_exception"

type searchGateway struct {
	client         *elasticsearch.Client
	compositeIndex string
	metricIndex    string
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
	return NewSearchGatewayWithIndices(addresses, "", "")
}

// NewSearchGatewayWithIndices selects an isolated dataset; empty names use the application defaults.
func NewSearchGatewayWithIndices(addresses []string, compositeIndex, metricIndex string) SearchGateway {
	if compositeIndex == "" {
		compositeIndex = types.CompositeIndexable
	}
	if metricIndex == "" {
		metricIndex = types.MetricIndexable
	}
	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: addresses,
	})

	if err != nil {
		log.Panicf("failed to create search gateway: %s", err)
	}

	return &searchGateway{
		client:         client,
		compositeIndex: compositeIndex, metricIndex: metricIndex,
	}
}

func (s *searchGateway) Search(query io.Reader) (entities []json.RawMessage, err error) {
	data, err := s.doSearch(query, s.compositeIndex)

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
	data, err := s.doSearch(query, s.metricIndex)

	if err != nil {
		return nil, errors.Wrap(err, "failed to perform aggregate search")
	}

	result := aggSearchResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, errors.Wrap(err, "failed to parse aggregate search output")
	}

	if len(result.Aggregations) != 1 {
		return nil, fmt.Errorf("expected one aggregation, got %d", len(result.Aggregations))
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
		defer res.Body.Close()
		return fmt.Errorf("failed to index entity (code %d): %s", res.StatusCode, res.String())
	}

	res.Body.Close()
	return nil
}

func (s *searchGateway) Declare(index types.Index) {
	request := esapi.IndicesCreateRequest{
		Index:  index.Name,
		Human:  true,
		Pretty: true,
		Body:   strings.NewReader(index.Spec),
	}

	res, err := request.Do(context.Background(), s.client)
	if err != nil {
		log.Panicf("failed to create index %s: %s", index.Name, err)
	}
	defer res.Body.Close()
	if res.StatusCode == 400 && strings.Contains(res.String(), indexExist) {
		return
	} else if res.StatusCode >= 300 {
		log.Panicf("failed to create index %s (code %d): %s", index.Name, res.StatusCode, res.String())
	}
}

func (s *searchGateway) IndexMany(entities []*types.Indexable) error {
	if len(entities) == 0 {
		return nil
	}
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	for _, entity := range entities {
		if err := encoder.Encode(map[string]interface{}{"index": map[string]interface{}{"_index": entity.Index, "_id": entity.ID}}); err != nil {
			return err
		}
		if err := encoder.Encode(entity.Data); err != nil {
			return err
		}
	}
	res, err := s.client.Bulk(&body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("bulk indexing: %s", res.String())
	}
	var result struct {
		Errors bool `json:"errors"`
		Items  []map[string]struct {
			Status int             `json:"status"`
			Error  json.RawMessage `json:"error"`
		} `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return err
	}
	if result.Errors {
		return fmt.Errorf("bulk indexing failed: %+v", result.Items)
	}
	return nil
}

func (s *searchGateway) doSearch(query io.Reader, index string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := s.client.Search(
		s.client.Search.WithContext(ctx),
		s.client.Search.WithBody(query),
		s.client.Search.WithIndex(index),
	)

	if err != nil {
		return nil, errors.Wrap(err, "failed to search entity")
	} else if res.StatusCode >= 300 {
		defer res.Body.Close()
		return nil, fmt.Errorf("failed to search entity (code %d): %s", res.StatusCode, res.String())
	}

	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)

	if err != nil {
		return nil, errors.Wrap(err, "failed to read search output")
	}

	return data, err
}
