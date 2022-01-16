package logger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"

	"github.com/elastic/go-elasticsearch"
	"github.com/iakinsey/delver/types/message"
	"github.com/pkg/errors"
)

type ElasticsearchLogger interface {
}

type elasticsearchLogger struct {
	client *elasticsearch.Client
	index  string
}

func NewElasticsearchLogger(addresses []string) Logger {
	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: addresses,
	})

	if err != nil {
		log.Panic(errors.Wrap(err, "failed to create elasticsearch client"))
	}

	return &elasticsearchLogger{
		client: client,
		index:  "resource",
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
	} else if res.StatusCode == 200 {
		return nil
	}

	data, err := ioutil.ReadAll(res.Body)

	if err != nil {
		return errors.Wrapf(err, "failed to parse error after logging resource")
	}

	return fmt.Errorf("failed to index resource (code %d): %s", res.StatusCode, string(data))
}
