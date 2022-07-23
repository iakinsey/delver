package gateway

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/filter"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/rpc"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/websocket"
)

type client struct {
	conn         *websocket.Conn
	filter       rpc.FilterParams
	streamFilter filter.StreamFilter
}

type clientStreamer struct {
	clients       map[string]client
	socketToUuid  map[websocket.Conn][]string
	searchGateway SearchGateway
}

type ClientStreamer interface {
	Start() error
	Publish(entities []*types.Indexable) error
}

func NewClientStreamer() ClientStreamer {
	conf := config.Get().ClientStreamer
	searchGateway := NewSearchGateway(conf.SearchAddresses)

	return &clientStreamer{
		clients:       make(map[string]client),
		searchGateway: searchGateway,
	}
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
		// TODO
		/*``
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

func (s *clientStreamer) Register(conn *websocket.Conn) error {
	filters, err := s.getFilter(conn)

	if err != nil {
		return err
	}

	s.socketToUuid[*conn] = make([]string, 0)

	for key, val := range filters {
		s.clients[key] = client{
			conn:         conn,
			filter:       val,
			streamFilter: filter.GetStreamFilter(val),
		}
		s.socketToUuid[*conn] = append(s.socketToUuid[*conn], key)
		c := s.clients[key]

		if preload, ok := c.filter.Options["preload"]; preload && ok {
			if err = s.Preload(c); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *clientStreamer) Unregister(conn *websocket.Conn) {
	uuids, ok := s.socketToUuid[*conn]

	// TODO should return error?
	if !ok {
		return
	}

	for _, u := range uuids {
		delete(s.clients, u)
	}

	delete(s.socketToUuid, *conn)
}

func (s *clientStreamer) Preload(c client) error {
	searchFilter := filter.GetSearchFilter(c.filter)
	reader, err := searchFilter.Perform()

	if err != nil {
		return errors.Wrap(err, "unable to get search preload filter")
	}

	entities, err := s.searchGateway.Search(reader)

	if err != nil {
		return errors.Wrap(err, "failed to perform preload search")
	}

	if len(entities) == 0 {
		return nil
	}

	data, err := s.applyTransforms(entities, c.filter)

	if err != nil {
		return errors.Wrap(err, "failed to transform preload search")
	}

	if _, err := c.conn.Write(data); err != nil {
		return errors.Wrap(err, "failed to publish preload data to client")
	}

	return nil
}

func (s *clientStreamer) applyTransforms(entities []json.RawMessage, filter rpc.FilterParams) ([]byte, error) {
	// TODO START HERE NEXT
	return nil, nil
}

func (s *clientStreamer) getFilter(conn *websocket.Conn) (map[string]rpc.FilterParams, error) {
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

	return s.decodeFilters(decoded)
}

func (s *clientStreamer) decodeFilters(message []byte) (map[string]rpc.FilterParams, error) {
	result := make(map[string]rpc.FilterParams)
	decoded := make(map[string]json.RawMessage)

	if err := json.Unmarshal(message, &decoded); err != nil {
		return nil, errors.Wrap(err, "failed to parse filter")
	}

	for key, val := range decoded {
		var filter rpc.Filter

		if err := json.Unmarshal(val, &filter); err != nil {
			return nil, errors.Wrap(err, "failed to parse filter part")
		}

		var fp rpc.FilterParams

		if err := json.Unmarshal(val, fp); err != nil {
			return nil, errors.Wrap(err, "failed to parse filter value")
		}

		var fq interface{}

		switch filter.DataType {
		case rpc.FilterTypeArticle:
			fq = rpc.ArticleFilterQuery{}
		case rpc.FilterTypePage:
			fq = rpc.PageFilterQuery{}
		case rpc.FilterTypeMetric:
			fq = rpc.MetricFilterQuery{}
		default:
			return nil, fmt.Errorf("unknown filter type %s", filter.DataType)
		}

		if err := json.Unmarshal(fp.RawQuery, fq); err != nil {
			return nil, errors.Wrap(err, "failed to parse filter query")
		}

		fp.Query = fq
		result[key] = fp
	}

	return result, nil
}
