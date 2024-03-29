package fetcher

import (
	"encoding/json"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/resource/objectstore"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/worker"
	"github.com/pkg/errors"
)

// TODO put these values into a config module
type HttpFetcherParams struct {
	MaxRetries  int                     `json:"max_retries"`
	ObjectStore objectstore.ObjectStore `json:"-" resource:"object_store"`
}

type httpFetcher struct {
	MaxRetries  int
	ObjectStore objectstore.ObjectStore
	Client      util.DelverHTTPClient
}

func NewHttpFetcher(args HttpFetcherParams) worker.Worker {
	return &httpFetcher{
		MaxRetries:  args.MaxRetries,
		ObjectStore: args.ObjectStore,
		Client:      util.NewHTTPClient(),
	}
}

func (s *httpFetcher) OnMessage(msg types.Message) (interface{}, error) {
	request := message.FetcherRequest{}

	if err := json.Unmarshal(msg.Message, &request); err != nil {
		return nil, errors.Wrap(err, "error parsing fetcher request")
	}

	response := message.FetcherResponse{
		FetcherRequest: request,
	}

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
