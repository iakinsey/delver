package gateway

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/rpc"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/websocket"
)

type clientStreamer struct{}

type ClientStreamer interface {
	Start() error
	Publish(entities []*types.Indexable) error
}

func NewClientStreamer() ClientStreamer {
	return &clientStreamer{}
}

func (s *clientStreamer) Start() error {
	conf := config.Get().Streamer

	if !conf.Enabled {
		return nil
	}

	handler := http.NewServeMux()
	l, err := net.Listen("tcp", conf.Address)

	if err != nil {
		return err
	}

	handler.Handle("/", websocket.Handler(func(conn *websocket.Conn) {
		/*
			if err := websocket.JSON.Receive(conn & message); err != nil {

			}
		*/
	}))

	log.Infof("streamer server listening on %s", conf.Address)
	log.Fatal(http.Serve(l, handler))

	return nil
}

func (s *clientStreamer) Publish(entities []*types.Indexable) error {
	return nil
}

func (s *clientStreamer) Register(conn *websocket.Conn) {

}

func (s *clientStreamer) Unregister(conn *websocket.Conn) {}

func (s *clientStreamer) OnConnect() {}

func (s *clientStreamer) getFilter(conn *websocket.Conn) (map[string]interface{}, error) {
	url := conn.Config().Location.Path
	tokens := strings.Split(url, "/")

	if len(tokens) == 0 {
		return nil, fmt.Errorf("unable to parse url: %s", url)
	}

	encoded := tokens[len(tokens)-1]
	var decoded []byte

	if _, err := base64.RawURLEncoding.Decode(decoded, []byte(encoded)); err != nil {
		return nil, errors.Wrap(err, "failed to parse encoded filter")
	}

	filter := make(map[string]interface{})

	if err := json.Unmarshal(decoded, &filter); err != nil {
		return nil, errors.Wrap(err, "failed to parse decoded filter")
	}

	return filter, nil
}

func (s *clientStreamer) decodeFilters(message []byte) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	decoded := make(map[string]json.RawMessage)

	if err := json.Unmarshal(message, &decoded); err != nil {
		return nil, errors.Wrap(err, "failed to parse filter")
	}

	for key, val := range decoded {
		var filter rpc.Filter

		if err := json.Unmarshal(val, &filter); err != nil {
			return nil, errors.Wrap(err, "failed to parse filter part")
		}

		var st interface{}

		switch filter.DataType {
		case rpc.FilterTypeArticle:
			st = &rpc.ArticleFilter{}
		case rpc.FilterTypePage:
			st = &rpc.PageFilter{}
		case rpc.FilterTypeMetric:
			st = &rpc.MetricFilter{}
		default:
			return nil, fmt.Errorf("unknown filter type %s", filter.DataType)
		}

		if err := setFilter(key, val, result, st); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func setFilter(key string, val json.RawMessage, result map[string]interface{}, st interface{}) error {
	if err := json.Unmarshal(val, st); err != nil {
		return errors.Wrap(err, "failed to parse filter value")
	}

	result[key] = st

	return nil
}
