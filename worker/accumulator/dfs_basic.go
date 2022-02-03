package accumulator

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/util/bloom"
	"github.com/iakinsey/delver/util/maps"
	"github.com/iakinsey/delver/worker"
)

type dfsBasicAccumulator struct {
	maxDepth        int
	urlStorePath    string
	visitedUrlsPath string
	urlStore        maps.Map
	visitedUrls     bloom.BloomFilter
}

const defaultBloomN = 10000000
const defaultBloomP = 0.1
const defaultBloomCount = 3

func NewDfsBasicAccumulator(urlStorePath string, visitedUrlsPath string, maxDepth int) worker.Worker {
	urlStore := maps.NewMultiHostMap(urlStorePath)
	visitedUrls, err := bloom.NewPersistentRollingBloomFilter(
		defaultBloomCount,
		defaultBloomN,
		defaultBloomP,
		visitedUrlsPath,
	)

	if err != nil {
		log.Fatalf("failed to create dfs basic visited url bloom filter %s", err)
	}

	w := &dfsBasicAccumulator{
		urlStorePath:    urlStorePath,
		visitedUrlsPath: visitedUrlsPath,
		urlStore:        urlStore,
		visitedUrls:     visitedUrls,
	}

	return w
}

func (s *dfsBasicAccumulator) OnMessage(msg types.Message) (interface{}, error) {
	composite := message.CompositeAnalysis{}

	if err := json.Unmarshal(msg.Message, &composite); err != nil {
		return nil, err
	}

	s.markVisited(composite)

	return types.MultiMessage{
		Values: s.prepareRequests(composite),
	}, nil
}

func (s *dfsBasicAccumulator) markVisited(composite message.CompositeAnalysis) {
	if err := s.visitedUrls.SetBytes([]byte(composite.URI)); err != nil {
		log.Printf("failed to mark url as visited: %s", composite.URI)
	}
}

func (s *dfsBasicAccumulator) prepareRequests(composite message.CompositeAnalysis) []interface{} {
	var result []interface{}
	var urlPairs [][2][]byte
	var toVisit [][]byte
	sourceSLD := util.GetSLD(composite.Host)

	for _, u := range composite.URIs {
		meta, err := url.Parse(u)

		if err != nil {
			log.Printf("failed to parse url: %s", u)
			continue
		}

		targetSLD := util.GetSLD(meta.Host)

		if sourceSLD == targetSLD && composite.Depth < s.maxDepth {
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
		} else if sourceSLD != targetSLD {
			// TODO
			// Consider whether or not to remove the if condition and allow matching hosts to
			// propagate to the urlStore.

			req := message.FetcherRequest{
				URI:    u,
				Origin: composite.URI,
			}

			if val, err := json.Marshal(req); err != nil {
				log.Printf("error preparing request: %s", err)
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
