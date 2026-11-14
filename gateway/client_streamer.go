package gateway

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

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

type subscriptionDelivery struct {
	mutex  sync.Mutex
	closed bool
}

type client struct {
	delivery     *subscriptionDelivery
	sender       *clientSender
	conn         *websocket.Conn
	filter       rpc.FilterParams
	streamFilter filter.StreamFilter
}

type clientStreamer struct {
	closed        bool
	server        *http.Server
	mutex         sync.RWMutex
	sendMutex     sync.Mutex
	aggregators   map[string]util.Aggregator
	clients       map[string]client
	searchGateway SearchGateway
	in            chan []*types.Indexable
}

type ClientStreamer interface {
	Start() error
	Publish(entities []*types.Indexable) error
}

func StartClientStreamer() {
	streamer := NewClientStreamer()

	if err := streamer.Start(); err != nil {
		log.Error(err)
	}
}

var defaultStreamer ClientStreamer
var defaultStreamerMutex sync.Mutex

func NewClientStreamer() ClientStreamer {
	defaultStreamerMutex.Lock()
	defer defaultStreamerMutex.Unlock()
	if defaultStreamer == nil {
		defaultStreamer = newClientStreamer()
	}
	return defaultStreamer
}
func newClientStreamer() ClientStreamer {
	conf := config.Get().Streamer
	var searchGateway SearchGateway
	if conf.Enabled {
		searchGateway = NewSearchGatewayWithIndices(conf.SearchAddresses, conf.CompositeIndex, conf.MetricIndex)
	}

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

	s.mutex.RLock()
	closed := s.closed
	s.mutex.RUnlock()
	if closed {
		return http.ErrServerClosed
	}
	handler := http.NewServeMux()
	l, err := net.Listen("tcp", conf.Address)

	if err != nil {
		return err
	}

	authGateway := NewUserGateway(config.Get().API.UserDBPath)
	defer func() {
		if closer, ok := authGateway.(interface{ Close() error }); ok {
			closer.Close()
		}
	}()
	handler.Handle("/", websocket.Server{Handshake: func(c *websocket.Config, r *http.Request) error {
		return validateStreamOrigin(r, conf.AllowedOrigins)
	}, Handler: func(conn *websocket.Conn) { s.serveSession(conn, authGateway) }})

	log.Infof("streamer server listening on %s", conf.Address)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	s.mutex.Lock()
	if s.closed {
		s.mutex.Unlock()
		l.Close()
		return http.ErrServerClosed
	}
	s.server = server
	s.mutex.Unlock()
	return server.Serve(l)
}

func (s *clientStreamer) Publish(indexables []*types.Indexable) error {
	entityMap := make(map[string][]*types.Indexable)
	var multiErr error

	// group entities by data type
	for _, entity := range indexables {
		if entity.Streamable {
			entityMap[entity.DataType] = append(entityMap[entity.DataType], entity)
		}
	}

	s.mutex.RLock()
	clients := make([]client, 0, len(s.clients))
	for _, c := range s.clients {
		clients = append(clients, c)
	}
	s.mutex.RUnlock()
	for _, c := range clients {
		if c.delivery != nil {
			c.delivery.mutex.Lock()
			if c.delivery.closed {
				c.delivery.mutex.Unlock()
				continue
			}
		}
		err := s.publishClient(c, entityMap)
		if c.delivery != nil {
			c.delivery.mutex.Unlock()
		}
		if err != nil {
			multiErr = multierror.Append(multiErr, err)
		}
	}
	return multiErr
}

func (s *clientStreamer) publishClient(c client, entityMap map[string][]*types.Indexable) error {
	entities, ok := entityMap[c.filter.DataType]
	if !ok {
		return nil
	}
	results, err := c.streamFilter.Perform(entities)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return nil
	}
	return s.send(c, results, false)
}

func (s *clientStreamer) Register(conn *websocket.Conn) (uuids []string, err error) {
	filters, err := s.getFilter(conn)

	if err != nil {
		return
	}

	if len(filters) == 0 {
		return nil, fmt.Errorf("at least one subscription is required")
	}
	sender := newClientSender(conn)
	for key, val := range filters {
		s.mutex.Lock()
		s.clients[key] = client{
			conn:         conn,
			sender:       sender,
			filter:       val,
			streamFilter: filter.GetStreamFilter(val),
		}

		uuids = append(uuids, key)
		c := s.clients[key]
		s.mutex.Unlock()

		if preload, ok := c.filter.Options["preload"]; preload && ok {
			if err = s.Preload(c); err != nil {
				return
			}
		}
	}

	return
}

func (s *clientStreamer) Unregister(uuids []string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, u := range uuids {
		if c, ok := s.clients[u]; ok {
			s.flushClient(c)
			c.sender.stop()
		}
		delete(s.clients, u)
	}
}

func (s *clientStreamer) Preload(c client) error {
	searchFilter := filter.GetSearchFilter(c.filter)
	reader, err := searchFilter.Perform()
	aggPreflight := searchFilter.IsAggregate()

	if err != nil {
		return errors.Wrap(err, "unable to get search preload filter")
	}

	var entities []json.RawMessage

	if aggPreflight {
		entities, err = s.searchGateway.SearchAggregate(reader)
	} else {
		entities, err = s.searchGateway.Search(reader)
	}

	if err != nil {
		return errors.Wrap(err, "failed to perform preload search")
	}

	if len(entities) >= 10000 && c.sender != nil {
		data, _ := json.Marshal([]map[string]string{{"callback": c.filter.Callback, "message": "History is limited to the latest 10,000 documents"}})
		c.sender.enqueue(types.ClientStreamerMessage{Type: "history_limit", Data: data})
	}
	if len(entities) == 0 {
		return nil
	}

	entities, err = searchFilter.Postprocess(entities)

	if err != nil {
		return errors.Wrap(err, "failed search preload postprocessing")
	}

	if err := s.send(c, entities, aggPreflight, true); err != nil {
		return errors.Wrap(err, "failed to publish preload data to client")
	}

	return nil
}

// TODO modify this function to perform the apply transforms mechanism
// Then make sure both stream and search paths go through it
func (s *clientStreamer) send(c client, entities []json.RawMessage, aggPreflight bool, preload ...bool) error {
	s.sendMutex.Lock()
	defer s.sendMutex.Unlock()
	data, err := s.applyTransforms(entities, c.filter, aggPreflight)
	if err == nil && len(preload) > 0 && preload[0] && c.filter.Agg != nil {
		data, err = s.finishPreload(entities, c.filter, aggPreflight, data)
	}

	if err != nil {
		return errors.Wrap(err, "failed to transform preload search")
	}

	if bytes.Equal(data, []byte("null")) || bytes.Equal(data, []byte("[]")) {
		return nil
	}
	msg := types.ClientStreamerMessage{
		Type: c.filter.DataType,
		Data: data,
	}

	if c.sender == nil {
		return fmt.Errorf("subscription has no sender")
	}
	if err := c.sender.enqueue(msg); err != nil {
		return errors.Wrap(err, "failed to send data to client")
	}

	return nil
}

func (s *clientStreamer) applyTransforms(entities []json.RawMessage, filter rpc.FilterParams, aggPreflight bool) ([]byte, error) {
	if (filter.Agg == nil || aggPreflight) && filter.Callback == "" {
		res, err := json.Marshal(entities)

		if err != nil {
			return nil, errors.Wrap(err, "failed to serialize entities while preparing output data")
		}

		return res, nil
	} else if filter.Agg == nil || aggPreflight {
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

	keyBytes, _ := json.Marshal(filter)
	key := filter.AggregationID + string(keyBytes)
	if s.aggregators == nil {
		s.aggregators = make(map[string]util.Aggregator)
	}
	agg := s.aggregators[key]
	if agg == nil {
		var err error
		agg, err = util.NewAggregator(
			filter.Agg.Name,
			filter.Agg.TimeField,
			filter.Agg.AggField,
			filter.Agg.TimeWindowSeconds,
		)

		if err != nil {
			return nil, errors.Wrap(err, "invalid aggregator params while preparing output data")
		}

		s.aggregators[key] = agg
	}
	var results []interface{}

	for _, rawEntity := range entities {
		var entityJson map[string]interface{}
		entity := make(map[string]float64)

		if err := json.Unmarshal(rawEntity, &entityJson); err != nil {
			return nil, errors.Wrap(err, "failed to parse entity while preparing aggregate output data")
		}

		if err := util.FlattenJSON(entityJson, "", entity); err != nil {
			return nil, errors.Wrap(err, "failed to flatten json map when preparing aggregate output data")
		}

		//func FlattenJSON(entity map[string]interface{}, prefix string, result map[string]float64) error {
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
			results = append(results, m2)
		} else {
			results = append(results, m)
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

	encoded := strings.Replace(tokens[len(tokens)-1], "_", "/", -1)
	decoded, err := base64.RawURLEncoding.DecodeString(tokens[len(tokens)-1])
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(encoded)
	}

	if err != nil {
		return nil, errors.Wrap(err, "failed to parse encoded filter")
	}

	return s.decodeFilters(decoded)
}

func (s *clientStreamer) decodeFilters(message []byte) (map[string]rpc.FilterParams, error) {
	decoded := make([]rpc.FilterParams, 0)
	result := make(map[string]rpc.FilterParams, 0)

	if err := json.Unmarshal(message, &decoded); err != nil {
		return nil, errors.Wrap(err, "failed to parse filter")
	}

	for _, fp := range decoded {
		if fp.DataType == rpc.FilterTypeMetric && fp.Agg != nil {
			if fp.Agg.Name == "" {
				fp.Agg.Name = "avg"
			}
			if fp.Agg.TimeWindowSeconds <= 0 {
				fp.Agg.TimeWindowSeconds = 1
			}
			if fp.Agg.TimeField == "" {
				fp.Agg.TimeField = "when"
			}
			if fp.Agg.AggField == "" {
				fp.Agg.AggField = "value"
			}
		}
		// If fp.RawQuery is of size 2, then it's liklely an empty map. Parsing can be skipped.
		if len(fp.RawQuery) == 0 {
			fp.RawQuery = json.RawMessage(`{}`)
		}

		switch fp.DataType {
		case rpc.FilterTypeComposite:
			cfq := rpc.CompositeFilterQuery{}

			if err := json.Unmarshal(fp.RawQuery, &cfq); err != nil {
				return nil, errors.Wrap(err, "failed to parse filter query")
			}

			fp.Query = cfq
		case rpc.FilterTypeMetric:
			mfq := rpc.MetricFilterQuery{}

			if err := json.Unmarshal(fp.RawQuery, &mfq); err != nil {
				return nil, errors.Wrap(err, "failed to parse filter query")
			}

			fp.Query = mfq
		default:
			return nil, fmt.Errorf("unknown filter type %s", fp.DataType)
		}

		result[string(types.NewV4())] = fp
	}

	return result, nil
}

func (s *clientStreamer) Close() error {
	s.mutex.Lock()
	s.closed = true
	server := s.server
	clients := s.clients
	s.clients = map[string]client{}
	s.mutex.Unlock()
	for _, c := range clients {
		s.flushClient(c)
		if c.sender != nil {
			c.sender.stop()
		} else {
			c.conn.Close()
		}
	}
	for _, c := range clients {
		if c.sender != nil {
			select {
			case <-c.sender.finished:
			case <-time.After(6 * time.Second):
				c.conn.Close()
			}
		}
	}
	if server != nil {
		return server.Close()
	}
	return nil
}
func ResetClientStreamer() {
	defaultStreamerMutex.Lock()
	defer defaultStreamerMutex.Unlock()
	defaultStreamer = nil
}

func (s *clientStreamer) flushClient(c client) {
	s.sendMutex.Lock()
	defer s.sendMutex.Unlock()
	keyBytes, _ := json.Marshal(c.filter)
	key := c.filter.AggregationID + string(keyBytes)
	agg := s.aggregators[key]
	delete(s.aggregators, key)
	if flush, ok := agg.(interface{ Flush() map[string]float64 }); ok {
		if result := flush.Flush(); result != nil && c.sender != nil {
			entity := map[string]interface{}{}
			for k, v := range result {
				entity[k] = v
			}
			if c.filter.Callback != "" {
				entity["callback"] = c.filter.Callback
			}
			data, _ := json.Marshal([]interface{}{entity})
			c.sender.enqueue(types.ClientStreamerMessage{Type: c.filter.DataType, Data: data})
		}
	}
}

func (s *clientStreamer) finishPreload(entities []json.RawMessage, filter rpc.FilterParams, preaggregated bool, data []byte) ([]byte, error) {
	keyBytes, _ := json.Marshal(filter)
	key := filter.AggregationID + string(keyBytes)
	if preaggregated {
		if len(entities) == 0 {
			return data, nil
		}
		var last types.Metric
		if err := json.Unmarshal(entities[len(entities)-1], &last); err != nil {
			return nil, err
		}
		timeField, aggField := filter.Agg.TimeField, filter.Agg.AggField
		if timeField == "" {
			timeField = "when"
		}
		if aggField == "" {
			aggField = "value"
		}
		agg, err := util.NewAggregator(filter.Agg.Name, timeField, aggField, filter.Agg.TimeWindowSeconds)
		if err != nil {
			return nil, err
		}
		if seed, ok := agg.(interface {
			Seed(float64, float64, float64)
		}); ok {
			count := float64(last.Samples)
			if count <= 0 {
				count = 1
			}
			seed.Seed(float64(last.When), last.Value, count)
		}
		if s.aggregators == nil {
			s.aggregators = map[string]util.Aggregator{}
		}
		s.aggregators[key] = agg
		return data, nil
	}
	agg := s.aggregators[key]
	snapshot, ok := agg.(interface{ Snapshot() map[string]float64 })
	if !ok {
		return data, nil
	}
	pending := snapshot.Snapshot()
	if pending == nil {
		return data, nil
	}
	var results []map[string]interface{}
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, err
	}
	point := map[string]interface{}{}
	for k, v := range pending {
		point[k] = v
	}
	if filter.Callback != "" {
		point["callback"] = filter.Callback
	}
	results = append(results, point)
	return json.Marshal(results)
}
