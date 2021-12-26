package fetcher

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/iakinsey/delver/gateway/streamstore"
	"github.com/iakinsey/delver/model"
	"github.com/iakinsey/delver/model/message"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/worker"
)

const defaultUserAgent = "delver"

type HttpFetcherArgs struct {
	UserAgent   string
	MaxRetries  int
	Timeout     time.Duration
	ProxyHost   string
	ProxyPort   string
	StreamStore streamstore.StreamStore
}

type httpFetcher struct {
	HttpFetcherArgs
}

func NewHttpFetcher(args HttpFetcherArgs) worker.Worker {
	return &httpFetcher{args}
}

func (s *httpFetcher) OnMessage(msg model.Message) (interface{}, error) {
	request := message.FetcherRequest{}
	response := message.FetcherResponse{}
	response.RequestID = request.RequestID
	response.URI = request.URI
	response.Protocol = request.Protocol

	if err := json.Unmarshal(msg.Message, &request); err != nil {
		return nil, err
	}

	s.doHttpRequestWithRetry(request, &response)

	return response, nil
}

func (s *httpFetcher) OnComplete() {}

func (s *httpFetcher) doHttpRequestWithRetry(request message.FetcherRequest, response *message.FetcherResponse) {
	var key types.UUID
	var err error

	start := time.Now()

	for i := 0; i < s.MaxRetries+1; i++ {
		key, err = s.doHttpRequest(request, response)

		if err == nil {
			response.StoreKey = key
			break
		}
	}

	// TODO ContentMD5
	response.Success = err == nil
	response.ElapsedTimeMs = time.Since(start).Milliseconds()

	if err != nil {
		response.Error = err.Error()
	}
}

func (s *httpFetcher) doHttpRequest(request message.FetcherRequest, response *message.FetcherResponse) (key types.UUID, err error) {
	client := &http.Client{Timeout: s.Timeout}
	req, err := http.NewRequest("GET", string(request.URI), nil)

	if err != nil {
		return key, err
	}

	req.Header.Set("User-Agent", s.UserAgent)

	res, err := client.Do(req)

	if err != nil {
		return key, err
	}

	response.HTTPCode = res.StatusCode
	key = types.NewV4()

	return key, s.StreamStore.Put(key, res.Body)
}
