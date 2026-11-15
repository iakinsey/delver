package accumulator

import (
	"encoding/json"
	"net/url"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/resource/bloom"
	"github.com/iakinsey/delver/resource/maps"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
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
	var URIs features.URIs
	composite := message.CompositeAnalysis{}

	if err := json.Unmarshal(msg.Message, &composite); err != nil {
		return nil, err
	}

	if !composite.Success {
		return nil, nil
	}
	if err := composite.Load(features.UrlField, &URIs); err != nil {
		return nil, errors.Wrap(err, "dfs basic accumulator")
	}

	requests, err := s.prepareRequests(composite, URIs)
	if err != nil {
		return nil, err
	}

	log.Printf("published %d requests from uri %s", len(requests), composite.URI)

	return types.MultiMessage{Values: requests}, nil
}

func (s *dfsBasicAccumulator) markVisited(composite message.CompositeAnalysis) {
	if err := s.visitedUrls.SetBytes([]byte(composite.URI)); err != nil {
		log.Errorf("failed to mark url as visited: %s", composite.URI)
	}
}

func (s *dfsBasicAccumulator) prepareRequests(composite message.CompositeAnalysis, URIs features.URIs) ([]interface{}, error) {
	var result []interface{}
	var urlPairs [][2][]byte
	var source string

	if meta, err := url.Parse(composite.URI); err == nil {
		source = util.GetSLDAndTLD(meta.Host)
	}

	for _, u := range URIs {
		meta, err := url.Parse(u)

		if err != nil {
			log.Errorf("failed to parse url: %s", u)
			continue
		}

		target := util.GetSLDAndTLD(meta.Host)

		if source == target && composite.Depth < s.maxDepth {
			// do not fall back after bloom filter check
			if s.visitedUrls == nil || !s.visitedUrls.ContainsString(u) {
				result = append(result, message.FetcherRequest{
					RequestID: types.NewV4(),
					URI:       u,
					Host:      meta.Host,
					Origin:    composite.URI,
					ScopeRoot: composite.ScopeRoot,
					Protocol:  types.ProtocolHTTP,
					Depth:     composite.Depth + 1,
				})
			}
		} else if source != target {
			// TODO
			// Consider whether or not to remove the if condition and allow matching hosts to
			// propagate to the urlStore.

			req := message.FetcherRequest{
				URI:       u,
				Origin:    composite.URI,
				ScopeRoot: composite.ScopeRoot,
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

	if err := s.urlStore.SetMany(urlPairs); err != nil {
		return nil, errors.Wrap(err, "persisting discovered URLs")
	}

	return result, nil
}

func (s *dfsBasicAccumulator) OnComplete() {

}

func (s *dfsBasicAccumulator) OnCommit(msg types.Message) error {
	var c message.CompositeAnalysis
	if err := json.Unmarshal(msg.Message, &c); err != nil {
		return err
	}
	if s.visitedUrls == nil {
		return nil
	}
	return s.visitedUrls.SetString(c.URI)
}
