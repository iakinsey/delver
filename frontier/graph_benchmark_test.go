package frontier

import (
	"context"
	"fmt"
	"github.com/iakinsey/delver/types/message"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

type graphResult struct {
	Requests, Origins, Unique int
	Elapsed                   time.Duration
}
type graphEvent struct {
	candidate Candidate
	end       time.Duration
}

// runGraph uses a deterministic four-slot HTTP model, one request per origin.
// Time is simulated; admission, scheduling, feedback, and persistence are real.
func runGraph(t testing.TB, shape, strategy string, budget int) graphResult {
	t.Helper()
	ctx := context.Background()
	s, err := OpenStore(StoreParams{Path: filepath.Join(t.TempDir(), "db"), OriginDelay: Lifetime(time.Nanosecond), MaxPending: 10000})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Admit(ctx, message.FetcherRequest{URI: "https://h000.test/0"}); err != nil {
		t.Fatal(err)
	}
	active := []graphEvent{}
	busy := map[string]bool{}
	origins := map[string]bool{}
	hashes := map[string]bool{}
	var now time.Duration
	var step uint64
	last, phase := "", ""
	dispatched := 0
	for dispatched < budget || len(active) > 0 {
		for dispatched < budget && len(active) < 4 {
			policy := strategy
			if strategy == "host_serial_baseline" {
				policy = "breadth_first"
			}
			cs, err := s.Candidates(ctx, policy, last, 1024)
			if err != nil {
				t.Fatal(err)
			}
			if strategy == "host_serial_baseline" {
				hasPhase := false
				for _, c := range cs {
					if c.Request.Host == phase {
						hasPhase = true
					}
				}
				if !hasPhase && !busy[phase] {
					phase = ""
				}
				if phase == "" && len(cs) > 0 {
					phase = cs[0].Request.Host
				}
			}
			eligible := []Candidate{}
			for _, c := range cs {
				if !busy[c.Request.Host] && (strategy != "host_serial_baseline" || c.Request.Host == phase) {
					eligible = append(eligible, c)
				}
			}
			if len(eligible) == 0 {
				break
			}
			scores, err := s.Scores(eligible)
			if err != nil {
				t.Fatal(err)
			}
			at := Strategy(policy).Select(eligible, last, step, scores)
			c, err := s.Reserve(ctx, eligible[at])
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Published(c); err != nil {
				t.Fatal(err)
			}
			latency := 50 * time.Millisecond
			if shape == "mixed_latency" && c.Request.Host == "h000.test" {
				latency = 500 * time.Millisecond
			}
			active = append(active, graphEvent{c, now + latency})
			busy[c.Request.Host] = true
			origins[c.Request.Host] = true
			last = c.Request.Host
			step++
			dispatched++
		}
		if len(active) == 0 {
			break
		}
		sort.Slice(active, func(i, j int) bool { return active[i].end < active[j].end })
		event := active[0]
		active = active[1:]
		now = event.end
		delete(busy, event.candidate.Request.Host)
		c := event.candidate
		u, _ := url.Parse(c.Request.URI)
		node, _ := strconv.Atoi(strings.TrimPrefix(u.Path, "/"))
		hash := c.Request.URI
		if shape == "duplicate_heavy" {
			hash = fmt.Sprintf("shared-%d", node%8)
		}
		hashes[hash] = true
		elapsed := int64(50)
		if shape == "mixed_latency" && c.Request.Host == "h000.test" {
			elapsed = 500
		}
		if err := s.Outcome(message.FetcherResponse{FetcherRequest: c.Request, Success: true, HTTPCode: 200, ElapsedTimeMs: elapsed, ContentMD5: hash, Header: map[string][]string{"Content-Type": {"text/html"}}}); err != nil {
			t.Fatal(err)
		}
		children := []message.FetcherRequest{}
		add := func(host string, n int) {
			children = append(children, message.FetcherRequest{URI: fmt.Sprintf("https://%s/%d", host, n), Depth: c.Request.Depth + 1})
		}
		switch shape {
		case "deep":
			if node < 2048 {
				add(c.Request.Host, node+1)
			}
		case "wide":
			if node == 0 {
				for i := 1; i <= 512; i++ {
					add(c.Request.Host, i)
				}
			}
		case "duplicate_heavy":
			for i := 1; i <= 32; i++ {
				for j := 0; j < 3; j++ {
					add(c.Request.Host, i)
				}
			}
		case "mixed_latency":
			if node == 0 && c.Request.Host == "h000.test" {
				for i := 1; i <= 256; i++ {
					add(c.Request.Host, i)
				}
				for h := 1; h <= 64; h++ {
					add(fmt.Sprintf("h%03d.test", h), 0)
				}
			} else if c.Request.Host != "h000.test" && node < 8 {
				add(c.Request.Host, node+1)
			}
		}
		if err := s.StageDiscovery(ctx, c.Request.URI, children); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DrainDiscovery(ctx, 1024); err != nil {
			t.Fatal(err)
		}
	}
	return graphResult{dispatched, len(origins), len(hashes), now}
}
func BenchmarkTraversalGraphs(b *testing.B) {
	for _, shape := range []string{"deep", "wide", "duplicate_heavy", "mixed_latency"} {
		for _, strategy := range []string{"host_serial_baseline", "breadth_first", "depth_first", "host_diverse", "adaptive"} {
			b.Run(shape+"/"+strategy, func(b *testing.B) {
				var r graphResult
				for i := 0; i < b.N; i++ {
					r = runGraph(b, shape, strategy, 128)
				}
				b.ReportMetric(float64(r.Requests), "requests")
				b.ReportMetric(float64(r.Origins), "origins")
				b.ReportMetric(float64(r.Unique), "unique_pages")
				b.ReportMetric(r.Elapsed.Seconds(), "simulated_seconds")
			})
		}
	}
}
func TestGraphCoverageAndBudgets(t *testing.T) {
	base := runGraph(t, "mixed_latency", "host_serial_baseline", 64)
	diverse := runGraph(t, "mixed_latency", "host_diverse", 64)
	if base.Requests != 64 || diverse.Requests != 64 || diverse.Origins <= base.Origins || diverse.Elapsed >= base.Elapsed {
		t.Fatal(base, diverse)
	}
}
