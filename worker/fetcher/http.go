package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/frontier"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/resource/objectstore"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/worker"
	"github.com/pkg/errors"
)

type HttpFetcherParams struct {
	ObjectStore objectstore.ObjectStore `json:"-" resource:"object_store"`
}

type httpFetcher struct {
	robots      frontier.ClosableFilter
	ObjectStore objectstore.ObjectStore
	Client      util.DelverHTTPClient
}

func NewHttpFetcher(args HttpFetcherParams) worker.Worker {
	client := util.NewHTTPClient()
	conf := config.Get().HTTPClient
	robots := frontier.NewMemoryRobots()
	util.SetHTTPPolicy(client, func(u *url.URL) error {
		if len(conf.AllowedDomains) > 0 {
			allowed := false
			for _, domain := range conf.AllowedDomains {
				if strings.EqualFold(u.Hostname(), domain) || strings.EqualFold(util.GetSLDAndTLD(strings.ToLower(u.Hostname())), domain) {
					allowed = true
				}
			}
			if !allowed {
				return fmt.Errorf("URL outside allowed domains: %s", u)
			}
		}
		if conf.RespectRobots {
			allowed, err := robots.IsAllowed(u.String())
			if err != nil {
				return err
			}
			if !allowed {
				return fmt.Errorf("robots disallows %s", u)
			}
		}
		return nil
	})
	return &httpFetcher{
		ObjectStore: args.ObjectStore,
		robots:      robots,
		Client:      client,
	}
}

func (s *httpFetcher) OnMessage(msg types.Message) (interface{}, error) {
	return s.OnMessageContext(context.Background(), msg)
}
func (s *httpFetcher) OnMessageContext(ctx context.Context, msg types.Message) (interface{}, error) {
	request := message.FetcherRequest{}

	if err := json.Unmarshal(msg.Message, &request); err != nil {
		return nil, errors.Wrap(err, "error parsing fetcher request")
	}

	response := message.FetcherResponse{
		FetcherRequest: request,
	}

	s.doHttpRequestWithRetry(ctx, request, &response)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return response, nil
}

func (s *httpFetcher) OnComplete() {
	if s.robots != nil {
		s.robots.Close()
	}
}

func (s *httpFetcher) doHttpRequestWithRetry(ctx context.Context, request message.FetcherRequest, response *message.FetcherResponse) {
	var key types.UUID
	var err error

	start := time.Now()
	response.Timestamp = start.Unix()

	key, err = s.doHttpRequest(ctx, request, response)

	if err == nil {
		response.StoreKey = key
	}

	response.Success = err == nil
	response.ElapsedTimeMs = time.Since(start).Milliseconds()

	if err != nil {
		response.Error = err.Error()
	}
}

func (s *httpFetcher) doHttpRequest(ctx context.Context, request message.FetcherRequest, response *message.FetcherResponse) (key types.UUID, err error) {
	var res *http.Response
	if client, ok := s.Client.(interface {
		PerformContext(context.Context, string) (*http.Response, error)
	}); ok {
		res, err = client.PerformContext(ctx, request.URI)
	} else {
		res, err = s.Client.Perform(request.URI)
	}

	if err != nil {
		return key, err
	}

	if res != nil {
		defer res.Body.Close()
	}

	if res == nil || res.Body == nil {
		return key, errors.New("empty HTTP response")
	}
	if res.Request != nil && res.Request.URL != nil {
		response.EffectiveURI = res.Request.URL.String()
	}
	response.HTTPCode = res.StatusCode
	response.Header = res.Header
	key = types.NewV4()

	log.Printf("GET %d %s", response.HTTPCode, request.URI)

	limit := config.Get().HTTPClient.MaxResponseBytes
	if limit <= 0 {
		limit = 16 << 20
	}
	hash, err := s.ObjectStore.Put(key, &boundedBody{reader: res.Body, remaining: limit})

	if err == nil {
		response.ContentMD5 = hash
	}

	return key, errors.Wrap(err, "store object failure")
}

type boundedBody struct {
	reader    io.Reader
	remaining int64
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		var probe [1]byte
		n, err := b.reader.Read(probe[:])
		if n > 0 {
			return 0, fmt.Errorf("response exceeds configured size limit")
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.reader.Read(p)
	b.remaining -= int64(n)
	return n, err
}
