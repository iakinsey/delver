package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elastic/go-elasticsearch"
	"github.com/elastic/go-elasticsearch/esapi"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
)

const indexExist = "resource_already_exists_exception"
const indexSpec = `{
	"settings":{},
	"mappings":{
		"properties": {
			"uri": {"type": "keyword"},
			"host": {"type": "keyword"},
			"origin": {"type": "keyword"},
			"protocol": {"type": "keyword"},
			"content_md5": {"type": "keyword"},
			"elapsed_time_ms": {"type": "keyword"},
			"error": {"type": "text"},
			"timestamp": {"type": "integer"},
			"http_code": {"type": "integer"},
			"text": {"type": "text"}
		}
	}
}`

type ElasticsearchLogger interface {
}

type elasticsearchLogger struct {
	client *elasticsearch.Client
	index  string
}

func NewElasticsearchLogger(addresses []string) Logger {
	index := "resource"

	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: addresses,
	})

	util.PanicIfErr(err, "failed to create elasticsearch client")
	util.PanicIfErr(initESIndex(client, index), "failed to initialize elasticsearch index")

	return &elasticsearchLogger{
		client: client,
		index:  index,
	}
}

func (s *elasticsearchLogger) LogResource(composite message.CompositeAnalysis) error {
	payload, err := json.Marshal(composite)

	if err != nil {
		return errors.Wrapf(err, "failed to serialize resource for logging")
	}

	res, err := s.client.Index(
		s.index,
		bytes.NewReader(payload),
		s.client.Index.WithDocumentID(string(composite.RequestID)),
	)

	if err != nil {
		return errors.Wrapf(err, "failed to index resource")
	} else if res.StatusCode >= 300 {
		return fmt.Errorf("failed to index resource (code %d): %s", res.StatusCode, res.String())
	}

	return nil
}

func initESIndex(client *elasticsearch.Client, index string) error {
	request := esapi.IndicesCreateRequest{
		Index:  index,
		Human:  true,
		Pretty: true,
		Body:   strings.NewReader(indexSpec),
	}

	if res, err := request.Do(context.Background(), client); err != nil {
		return errors.Wrap(err, "failed to create index")
	} else if res.StatusCode == 400 && strings.Contains(res.String(), indexExist) {
		return nil
	} else if res.StatusCode >= 300 {
		return fmt.Errorf("failed to create index (code %d): %s", res.StatusCode, res.String())
	}

	return nil
}
