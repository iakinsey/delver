package accumulator

import (
	"encoding/json"
	"fmt"
	"net/url"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/resource/bloom"
	"github.com/iakinsey/delver/resource/maps"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/worker"
)

type dfsBasicAccumulator struct {
	maxDepth    int
	urlStore    maps.Map
	visitedUrls bloom.BloomFilter
}

type DfsBasicAccumulatorParams struct {
	UrlStore    maps.Map          `json:"-" resource:"url_store"`
	VisitedUrls bloom.BloomFilter `json:"-" resource:"visited_urls"`
	MaxDepth    int               `json:"max_depth"`
}

func NewDfsBasicAccumulator(params DfsBasicAccumulatorParams) worker.Worker {
	return &dfsBasicAccumulator{
		urlStore:    params.UrlStore,
		visitedUrls: params.VisitedUrls,
		maxDepth:    params.MaxDepth,
	}
}

func (s *dfsBasicAccumulator) OnMessage(msg types.Message) (interface{}, error) {
	composite := message.CompositeAnalysis{}

	if err := json.Unmarshal(msg.Message, &composite); err != nil {
		return nil, err
	}

	s.markVisited(composite)

	requests := s.prepareRequests(composite)

	log.Printf("published %d requests from uri %s", len(requests), composite.URI)

	return types.MultiMessage{Values: requests}, nil
}

func (s *dfsBasicAccumulator) markVisited(composite message.CompositeAnalysis) {
	if err := s.visitedUrls.SetBytes([]byte(composite.URI)); err != nil {
		log.Errorf("failed to mark url as visited: %s", composite.URI)
	}
}

func (s *dfsBasicAccumulator) prepareRequests(composite message.CompositeAnalysis) []interface{} {
	var result []interface{}
	var urlPairs [][2][]byte
	var toVisit [][]byte
	var source string
	URIs := composite.Get(message.UrlExtractor).([]interface{})

	if meta, err := url.Parse(composite.URI); err == nil {
		source = util.GetSLDAndTLD(meta.Host)
	}

	for _, ui := range URIs {
		u := ui.(string)
		meta, err := url.Parse(u)

		if err != nil {
			log.Errorf("failed to parse url: %s", u)
			continue
		}

		target := util.GetSLDAndTLD(meta.Host)

		if source == target && composite.Depth < s.maxDepth {
			// do not fall back after bloom filter check
			if !s.visitedUrls.ContainsString(u) {
				result = append(result, message.FetcherRequest{
					RequestID: types.NewV4(),
					URI:       u,
					Host:      meta.Host,
					Origin:    composite.URI,
					Protocol:  types.ProtocolHTTP,
					Depth:     composite.Depth + 1,
				})
				toVisit = append(toVisit, []byte(u))
			}
		} else if source != target {
			// TODO
			// Consider whether or not to remove the if condition and allow matching hosts to
			// propagate to the urlStore.

			req := message.FetcherRequest{
				URI:    u,
				Origin: composite.URI,
			}

			if val, err := json.Marshal(req); err != nil {
				log.Errorf("error preparing request: %s", err)
			} else {
				urlPairs = append(urlPairs, [2][]byte{
					[]byte(u),
					val,
				})
			}
		}
	}

	if err := s.visitedUrls.SetMany(toVisit); err != nil {
		fmt.Printf("error saving urls to visit: %s", err)
	}

	if err := s.urlStore.SetMany(urlPairs); err != nil {
		fmt.Printf("error saving urls in urlStore: %s", err)
	}

	return result
}

func (s *dfsBasicAccumulator) OnComplete() {
	s.urlStore.Close()
	s.visitedUrls.Close()
}
