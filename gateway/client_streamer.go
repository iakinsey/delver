package gateway

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/filter"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/rpc"
	"github.com/iakinsey/delver/util"
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
	searchGateway SearchGateway
	in            chan []*types.Indexable
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
		in:            make(chan []*types.Indexable),
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
		uuids, err := s.Register(conn)

		log.Info("Connect %s", conn.RemoteAddr().String())

		if err != nil && uuids != nil {
			log.Errorf("failed to register client: %s", err)
		} else {
			// wait until client erroneously sends data or disconnects
			conn.Read(make([]byte, 1))
		}

		s.Unregister(uuids)
		log.Info("Disconnect %s", conn.RemoteAddr().String())
	}))

	log.Infof("streamer server listening on %s", conf.Address)
	log.Fatal(http.Serve(l, handler))

	return nil
}

func (s *clientStreamer) Publish(indexables []*types.Indexable) error {
	entityMap := make(map[string][]*types.Indexable)
	var multiErr error

	// group entities by data type
	for _, entity := range indexables {
		entityMap[entity.DataType] = append(entityMap[entity.DataType], entity)
	}

	for _, c := range s.clients {
		entities, ok := entityMap[c.filter.DataType]

		if !ok {
			continue
		}

		results, err := c.streamFilter.Perform(entities)

		if err != nil {
			multierror.Append(multiErr, err)
		}

		if results == nil {
			continue
		}

		if err = s.send(c, results); err != nil {
			multierror.Append(multiErr, err)
		}
	}

	return multiErr
}

func (s *clientStreamer) Register(conn *websocket.Conn) (uuids []string, err error) {
	filters, err := s.getFilter(conn)

	if err != nil {
		return
	}

	for key, val := range filters {
		s.clients[key] = client{
			conn:         conn,
			filter:       val,
			streamFilter: filter.GetStreamFilter(val),
		}

		uuids = append(uuids, key)
		c := s.clients[key]

		if preload, ok := c.filter.Options["preload"]; preload && ok {
			if err = s.Preload(c); err != nil {
				return
			}
		}
	}

	return
}

func (s *clientStreamer) Unregister(uuids []string) {
	for _, u := range uuids {
		delete(s.clients, u)
	}
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

	if err := s.send(c, entities); err != nil {
		return errors.Wrap(err, "failed to publish preload data to client")
	}

	return nil
}

// TODO modify this function to perform the apply transforms mechanism
// Then make sure both stream and search paths go through it
func (s *clientStreamer) send(c client, entities []json.RawMessage) error {
	data, err := s.applyTransforms(entities, c.filter)

	if err != nil {
		return errors.Wrap(err, "failed to transform preload search")
	}

	msg := types.ClientStreamerMessage{
		Type: c.filter.DataType,
		Data: data,
	}

	if err := websocket.JSON.Send(c.conn, msg); err != nil {
		return errors.Wrap(err, "failed to send data to client")
	}

	return nil
}

func (s *clientStreamer) applyTransforms(entities []json.RawMessage, filter rpc.FilterParams) ([]byte, error) {
	if filter.Agg == nil && filter.Callback == "" {
		res, err := json.Marshal(entities)

		if err != nil {
			return nil, errors.Wrap(err, "failed to serialize entities while preparing output data")
		}

		return res, nil
	} else if filter.Agg == nil {
		var preparedEntities []map[string]interface{}

		for _, rawEntity := range entities {
			var preparedEntity map[string]interface{}

			if err := json.Unmarshal(rawEntity, &preparedEntity); err != nil {
				return nil, errors.Wrap(err, "failed to parse entity while preparing output data")
			}

			preparedEntity["callback"] = filter.Callback
			preparedEntities = append(preparedEntities, preparedEntity)
		}

		res, err := json.Marshal(preparedEntities)

		if err != nil {
			return nil, errors.Wrap(err, "failed to serialize entity while preparing output data")
		}

		return res, nil
	}

	agg, err := util.NewAggregator(
		filter.Agg.Name,
		filter.Agg.TimeField,
		filter.Agg.AggField,
		filter.Agg.TimeWindowSeconds,
	)

	if err != nil {
		return nil, errors.Wrap(err, "invalid aggregator params while preparing output data")
	}

	var results []interface{}

	for _, rawEntity := range entities {
		var entity map[string]float64

		if err := json.Unmarshal(rawEntity, &entity); err != nil {
			return nil, errors.Wrap(err, "failed to parse entity while preparing aggregate output data")
		}

		m := agg.Perform(entity)

		if m == nil {
			continue
		}

		if filter.Callback != "" {
			m2 := make(map[string]interface{})

			for k, v := range m {
				m2[k] = v
			}

			m2["callback"] = filter.Callback
			results = append(results, entity)
		} else {
			results = append(results, entity)
		}
	}

	res, err := json.Marshal(results)

	if err != nil {
		return nil, errors.Wrap(err, "failed to serialize entities while preparing aggregate output data")
	}

	return res, nil
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

		if err := json.Unmarshal(val, &fp); err != nil {
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
