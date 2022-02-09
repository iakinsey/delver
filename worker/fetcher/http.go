package fetcher

import (
	"encoding/json"
	"log"
	"time"

	"github.com/iakinsey/delver/gateway/objectstore"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/worker"
	"github.com/pkg/errors"
)

// TODO put these values into a config module
type HttpFetcherArgs struct {
	MaxRetries  int
	ObjectStore objectstore.ObjectStore
	Client      *util.DelverHTTPClient
}

type httpFetcher struct {
	HttpFetcherArgs
}

func NewHttpFetcher(args HttpFetcherArgs) worker.Worker {
	return &httpFetcher{args}
}

func (s *httpFetcher) OnMessage(msg types.Message) (interface{}, error) {
	request := message.FetcherRequest{}
	response := message.FetcherResponse{}

	if err := json.Unmarshal(msg.Message, &request); err != nil {
		return nil, errors.Wrap(err, "error parsing fetcher request")
	}

	response.RequestID = request.RequestID
	response.URI = request.URI
	response.Protocol = request.Protocol

	s.doHttpRequestWithRetry(request, &response)

	return response, nil
}

func (s *httpFetcher) OnComplete() {}

func (s *httpFetcher) doHttpRequestWithRetry(request message.FetcherRequest, response *message.FetcherResponse) {
	var key types.UUID
	var err error

	start := time.Now()
	response.Timestamp = start.Unix()

	key, err = s.doHttpRequest(request, response)

	if err == nil {
		response.StoreKey = key
	}

	response.Success = err == nil
	response.ElapsedTimeMs = time.Since(start).Milliseconds()

	if err != nil {
		response.Error = err.Error()
	}
}

func (s *httpFetcher) doHttpRequest(request message.FetcherRequest, response *message.FetcherResponse) (key types.UUID, err error) {
	res, err := s.Client.Perform(request.URI)

	if err != nil {
		return key, err
	}

	if res != nil {
		defer res.Body.Close()
	}

	response.HTTPCode = res.StatusCode
	response.Header = res.Header
	key = types.NewV4()

	log.Printf("GET %d %s", response.HTTPCode, request.URI)

	hash, err := s.ObjectStore.Put(key, res.Body)

	if err == nil {
		response.ContentMD5 = hash
	}

	return key, errors.Wrap(err, "store object failure")
}
