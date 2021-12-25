package fetcher

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/iakinsey/delver/model"
	"github.com/iakinsey/delver/model/args"
	"github.com/iakinsey/delver/model/request"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/worker"
)

const defaultUserAgent = "delver"

type httpFetcher struct {
	args.HTTPFetcherArgs
}

func NewHTTPFetcher(fetcherArgs args.HTTPFetcherArgs) (worker.Worker, error) {
	if fetcherArgs.UserAgent == "" {
		fetcherArgs.UserAgent = defaultUserAgent
	}

	if fetcherArgs.StreamStore == nil {
		return nil, errors.New("HTTPFetcher requires StreamStore argument")
	}

	return &httpFetcher{fetcherArgs}, nil
}

func (s *httpFetcher) OnMessage(message model.Message) error {
	fetcherRequest := request.FetcherRequest{}

	if err := json.Unmarshal(message.Message, &fetcherRequest); err != nil {
		return err
	}
	return nil
}

func (s *httpFetcher) OnComplete() {}

func (s *httpFetcher) doHttpRequestWithRetry(uri string) (key types.UUID, err error) {
	for i := 0; i < s.MaxRetries+1; i++ {
		key, err := s.doHttpRequest(uri)

		if err == nil {
			return key, nil
		}
	}

	return key, err
}

func (s *httpFetcher) doHttpRequest(uri string) (key types.UUID, err error) {
	client := &http.Client{Timeout: s.Timeout}
	req, err := http.NewRequest("GET", uri, nil)

	if err != nil {
		return key, err
	}

	req.Header.Set("User-Agent", s.UserAgent)

	res, err := client.Do(req)

	if err != nil {
		return key, err
	}

	key = types.NewV4()

	return key, s.StreamStore.Put(key, res.Body)
}
