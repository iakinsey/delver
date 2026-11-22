package frontier

import (
	"context"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"math"
	"sort"
	"time"
)

// FrontierStore keeps policy code independent from the storage engine.
type FrontierStore interface {
	StageDiscovery(context.Context, string, []message.FetcherRequest) error
	DrainDiscovery(context.Context, int) (int, error)
	Candidates(context.Context, string, string, int) ([]Candidate, error)
	CleanupInterval() time.Duration
	Scores([]Candidate) (map[string]float64, error)
	Admit(context.Context, message.FetcherRequest) error
	Pending(context.Context, string, int) ([]Candidate, string, error)
	Reserve(context.Context, Candidate) (Candidate, error)
	Published(Candidate) error
	Outcome(message.FetcherResponse) error
	Cleanup(context.Context, time.Time) (int, error)
}

// Selector is a replaceable traversal policy over a bounded candidate window.
type Selector interface {
	Select([]Candidate, string, uint64, map[string]float64) int
}
type Strategy string

func (s Strategy) Select(cs []Candidate, last string, step uint64, scores map[string]float64) int {
	if len(cs) == 0 {
		return -1
	}
	if s == "host_diverse" || (s == "adaptive" && step%5 == 4) {
		hosts := map[string]bool{}
		for _, c := range cs {
			hosts[util.SiteKey(c.Request.Host)] = true
		}
		keys := make([]string, 0, len(hosts))
		for h := range hosts {
			keys = append(keys, h)
		}
		sort.Strings(keys)
		chosen := keys[0]
		for _, h := range keys {
			if h > last {
				chosen = h
				break
			}
		}
		best := -1
		for i, c := range cs {
			if util.SiteKey(c.Request.Host) == chosen && (best < 0 || c.Request.Depth < cs[best].Request.Depth) {
				best = i
			}
		}
		return best
	}
	best := 0
	value := math.Inf(-1)
	for i, c := range cs {
		v := -float64(c.Request.Depth)
		if s == "depth_first" {
			v = float64(c.Request.Depth)
		}
		if s == "adaptive" {
			v = scores[c.Request.Host]
		}
		if v > value {
			value = v
			best = i
		}
	}
	return best
}
