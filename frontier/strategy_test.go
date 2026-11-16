package frontier

import (
	"github.com/iakinsey/delver/types/message"
	"testing"
)

func TestStrategies(t *testing.T) {
	cs := []Candidate{{Request: message.FetcherRequest{Host: "a", Depth: 2}}, {Request: message.FetcherRequest{Host: "b", Depth: 1}}, {Request: message.FetcherRequest{Host: "c", Depth: 3}}}
	for _, tc := range []struct {
		s    Strategy
		last string
		step uint64
		want int
	}{{"breadth_first", "", 0, 1}, {"depth_first", "", 0, 2}, {"host_diverse", "b", 0, 2}, {"host_diverse", "c", 0, 0}, {"adaptive", "", 0, 1}, {"adaptive", "b", 4, 2}} {
		if got := tc.s.Select(cs, tc.last, tc.step, map[string]float64{"b": 10}); got != tc.want {
			t.Fatalf("%+v got %d", tc, got)
		}
	}
}
