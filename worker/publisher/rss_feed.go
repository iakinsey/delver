package publisher

import (
	"context"
	"errors"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/frontier"
	"io"
	"net/http"
	"net/url"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/worker"
	"github.com/mmcdole/gofeed"
)

type rssFeedPublisher struct {
	flow                *flow.Controller
	allowedHostSuffixes []string
	robots              frontier.ClosableFilter
	maxBytes            int64
	concurrency         int
	uris                []string
	client              util.DelverHTTPClient
	healthMu            sync.Mutex
	failures            map[string]string
}

type RssFeedPublisherParams struct {
	MaxConcurrency int      `json:"max_concurrency"`
	Uris           []string `json:"uris"`
}

func NewRssFeedPublisher(params RssFeedPublisherParams) worker.Worker {
	return NewRssFeedPublisherWithConfig(params, config.Get())
}
func NewRssFeedPublisherWithConfig(params RssFeedPublisherParams, c config.Config) worker.Worker {
	suffixes, _ := util.NormalizeHostSuffixes(c.HTTPClient.AllowedHostSuffixes)
	client := util.NewHTTPClientWithConfig(c.HTTPClient)
	robots := frontier.NewMemoryRobotsWithConfig(c)
	if c.HTTPClient.RespectRobots {
		util.SetHTTPPolicyContext(client, func(ctx context.Context, u *url.URL) error {
			ok, err := robots.IsAllowedContext(ctx, u.String())
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("robots disallows feed URL")
			}
			return nil
		})
	}
	return &rssFeedPublisher{
		flow:                c.HTTPClient.Flow,
		allowedHostSuffixes: suffixes,
		robots:              robots,
		uris:                params.Uris,
		concurrency:         params.MaxConcurrency,
		client:              client,
		maxBytes:            c.HTTPClient.MaxResponseBytes,
	}
}

func (s *rssFeedPublisher) OnMessage(msg types.Message) (interface{}, error) {
	return s.OnMessageContext(context.Background(), msg)
}

func (s *rssFeedPublisher) OnMessageContext(ctx context.Context, msg types.Message) (interface{}, error) {
	var messages []interface{}
	done := make(chan []interface{}, len(s.uris))
	concurrency := s.concurrency
	if concurrency <= 0 {
		concurrency = 8
	}
	slots := make(chan struct{}, concurrency)
	var active sync.WaitGroup
	for _, uri := range s.uris {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			active.Wait()
			return nil, ctx.Err()
		}
		active.Add(1)
		go func(uri string) {
			defer active.Done()
			defer func() { <-slots }()
			s.getRssUrlsContext(ctx, uri, done)
		}(uri)
	}
	active.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	close(done)
	for batch := range done {
		messages = append(messages, batch...)
	}
	return types.MultiMessage{Values: messages}, nil
}

func (s *rssFeedPublisher) getRssUrls(feedUri string, done chan []interface{}) {
	s.getRssUrlsContext(context.Background(), feedUri, done)
}

func (s *rssFeedPublisher) getRssUrlsContext(ctx context.Context, feedUri string, done chan []interface{}) {
	var result []interface{}
	var failure error
	defer func() {
		s.healthMu.Lock()
		defer s.healthMu.Unlock()
		if s.failures == nil {
			s.failures = map[string]string{}
		}
		if failure != nil {
			s.failures[feedUri] = failure.Error()
		} else {
			delete(s.failures, feedUri)
		}
	}()
	var res *http.Response
	var err error
	if client, ok := s.client.(interface {
		PerformContext(context.Context, string) (*http.Response, error)
	}); ok {
		res, err = client.PerformContext(ctx, feedUri)
	} else {
		res, err = s.client.Perform(feedUri)
	}
	if err == nil && (res == nil || res.Body == nil) {
		err = fmt.Errorf("empty feed response")
	}

	if err != nil {
		if errors.Is(err, util.ErrFileExtensionExcluded) {
			if s.flow != nil {
				s.flow.Counter("feed_policy_skips", 1)
			}
			done <- result
			return
		}
		failure = err
		log.Errorf("failed to perform http request: %s", err)
		done <- result
		return
	}

	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		failure = fmt.Errorf("HTTP %d", res.StatusCode)
		log.Errorf("feed %s returned HTTP %d", feedUri, res.StatusCode)
		done <- result
		return
	}
	parser := gofeed.NewParser()
	limit := s.maxBytes
	if limit <= 0 {
		limit = 16 << 20
	}
	feed, err := parser.Parse(&feedBody{ctx: ctx, reader: res.Body, remaining: limit})

	if err != nil {
		failure = err
		log.Errorf("failed to parse RSS feed: %s", err)
		done <- result
		return
	}

	for _, item := range feed.Items {
		links := append([]string(nil), item.Links...)
		if item.Link != "" {
			links = append(links, item.Link)
		}
		for _, uri := range util.DedupeStrSlice(links) {
			if !util.URLHostnameAllowed(uri, s.allowedHostSuffixes) {
				continue
			}
			meta, err := url.Parse(uri)

			if err != nil || meta.Host == "" || (meta.Scheme != "http" && meta.Scheme != "https") {
				log.Errorf("failed to parse url: %s for feed %s", uri, feedUri)
				continue
			}

			result = append(result, message.FetcherRequest{
				RequestID: types.NewV4(),
				URI:       uri,
				Host:      meta.Host,
				Origin:    feedUri,
				Protocol:  types.ProtocolHTTP,
				Depth:     1,
			})
		}
	}
	done <- result
}

func (s *rssFeedPublisher) OnComplete() {
	if s.robots != nil {
		s.robots.Close()
	}
	if client, ok := s.client.(interface{ Close() error }); ok {
		client.Close()
	}
}

func (s *rssFeedPublisher) Health() string {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	if len(s.failures) == 0 {
		return ""
	}
	return fmt.Sprint(s.failures)
}

type feedBody struct {
	ctx       context.Context
	reader    io.Reader
	remaining int64
}

func (r *feedBody) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.remaining == 0 {
		var probe [1]byte
		n, err := r.reader.Read(probe[:])
		if n > 0 {
			return 0, fmt.Errorf("feed exceeds configured response-size limit")
		}
		return n, err
	}
	if int64(len(buffer)) > r.remaining {
		buffer = buffer[:r.remaining]
	}
	n, err := r.reader.Read(buffer)
	r.remaining -= int64(n)
	return n, err
}
