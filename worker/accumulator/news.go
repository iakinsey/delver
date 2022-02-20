package accumulator

import (
	"encoding/json"
	"net/url"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/worker"
)

const maxDepth = 1

type newsAccumulator struct {
	maxDepth  int
	robots    frontier.Filter
	newsQueue queue.Queue
}

type NewsAccumulatorParams struct {
	NewsQueue queue.Queue     `json:"-" resource:"news_queue"`
	Robots    frontier.Filter `json:"-" resource:"robots"`
}

func NewNewsAccumulator(params NewsAccumulatorParams) worker.Worker {
	return &newsAccumulator{
		maxDepth:  maxDepth,
		robots:    params.Robots,
		newsQueue: params.NewsQueue,
	}
}

func (s *newsAccumulator) OnMessage(msg types.Message) (interface{}, error) {
	composite := message.CompositeAnalysis{}

	if err := json.Unmarshal(msg.Message, &composite); err != nil {
		return nil, err
	}

	urls := s.processUrls(composite)
	s.processArticle(composite)

	log.Printf("published %d requests for uri %s", len(urls), composite.URI)

	return types.MultiMessage{
		Values: urls,
	}, nil
}

func (s *newsAccumulator) processUrls(composite message.CompositeAnalysis) []interface{} {
	if composite.Depth >= s.maxDepth {
		return nil
	}

	var results []interface{}
	originParsed, err := url.Parse(composite.URI)

	if err != nil {
		log.Errorf("Unable to parse URI %s", composite.URI)
		return nil
	}

	origin := originParsed.Host
	count := 0

	for _, u := range composite.URIs {
		parsed, err := url.Parse(u)

		if err != nil || parsed.Host != origin {
			continue
		}

		if allowed, err := s.robots.IsAllowed(u); err != nil {
			log.Errorf("Failed to get robots info for URL %s: %s", u, err)
			continue
		} else if !allowed {
			continue
		}

		count += 1
		results = append(results, message.FetcherRequest{
			RequestID: types.NewV4(),
			URI:       u,
			Host:      parsed.Host,
			Origin:    composite.URI,
			Protocol:  types.ProtocolHTTP,
			Depth:     composite.Depth + 1,
		})
	}

	if count > 0 {
		log.Printf("Published %d urls", count)
	}

	return results
}

func (s *newsAccumulator) processArticle(composite message.CompositeAnalysis) {
	if composite.Depth == 0 {
		return
	}

	msg, err := json.Marshal(composite)

	if err != nil {
		log.Errorf("failed to serialize message segment for URI: %s", composite.URI)
		return
	}

	article := types.Message{
		ID:          string(types.NewV4()),
		MessageType: types.CompositeAnalysisType,
		Message:     json.RawMessage(msg),
	}

	if err := s.newsQueue.Put(article, 0); err != nil {
		log.Errorf("Failed to log article: %s", composite.URI)
	}
}

func (s *newsAccumulator) OnComplete() {}
