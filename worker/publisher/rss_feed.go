package publisher

import (
	"net/url"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/worker"
	"github.com/mmcdole/gofeed"
)

type rssFeedPublisher struct {
	uris   []string
	client *util.DelverHTTPClient
}

type RssFeedPublisherParams struct {
	Uris []string `json:"uris"`
}

func NewRssFeedPublisher(params RssFeedPublisherParams) worker.Worker {
	return &rssFeedPublisher{
		uris:   params.Uris,
		client: util.NewHTTPClient(),
	}
}

func (s *rssFeedPublisher) OnMessage(msg types.Message) (interface{}, error) {
	var messages []interface{}
	done := make(chan []interface{}, len(s.uris))

	for _, uri := range s.uris {
		go s.getRssUrls(uri, done)
	}

	for i := 0; i < len(s.uris); i++ {
		messages = append(messages, <-done...)
	}

	log.Errorf("published %d requests from RSS feeds", len(messages))

	return types.MultiMessage{
		Values: messages,
	}, nil
}

func (s *rssFeedPublisher) getRssUrls(feedUri string, done chan []interface{}) {
	var result []interface{}
	res, err := s.client.Perform(feedUri)

	if err != nil {
		log.Errorf("failed to perform http request: %s", err)
		done <- result
		return
	}

	parser := gofeed.NewParser()
	feed, err := parser.Parse(res.Body)

	if err != nil {
		log.Errorf("failed to parse RSS feed: %s", err)
		done <- result
		return
	}

	for _, item := range feed.Items {
		for _, uri := range item.Links {
			meta, err := url.Parse(uri)

			if err != nil {
				log.Errorf("failed to parse url: %s for feed %s", uri, feedUri)
				continue
			}

			result = append(result, message.FetcherRequest{
				RequestID: types.NewV4(),
				URI:       uri,
				Host:      meta.Host,
				Origin:    feedUri,
				Protocol:  types.ProtocolHTTP,
				Depth:     0,
			})
		}
	}
	done <- result
}

func (s *rssFeedPublisher) OnComplete() {}
