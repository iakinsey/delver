// Package trap contains bounded, name-independent structural observations.
package trap

import (
	"context"
	"fmt"
	"github.com/iakinsey/delver/site"
	"golang.org/x/net/html"
	"hash/fnv"
	"io"
	"net/url"
	"sort"
	"strings"
)

type Settings struct {
	CrossSiteLinks float64 `json:"cross_site_links"`
	FreshLinks     float64 `json:"fresh_links"`
	Enabled        bool    `json:"enabled"`
	Automatic      bool    `json:"automatic"`
	SiteWindow     int     `json:"site_window"`
	FamilyWindow   int     `json:"family_window"`
	Similarity     float64 `json:"similarity"`
	Redundancy     float64 `json:"redundancy"`
	Novelty        float64 `json:"novelty"`
	MinimumSamples int     `json:"minimum_samples"`
	MinimumLinks   int     `json:"minimum_links"`
}

func Default() Settings {
	return Settings{CrossSiteLinks: .5, FreshLinks: .5, Enabled: true, Automatic: true, SiteWindow: 20, FamilyWindow: 100, Similarity: .7, Redundancy: .8, Novelty: .05, MinimumSamples: 5, MinimumLinks: 8}
}
func (s Settings) Validate() error {
	if s.CrossSiteLinks < 0 || s.CrossSiteLinks > 1 || s.FreshLinks < 0 || s.FreshLinks > 1 || s.SiteWindow < 3 || s.SiteWindow > 200 || s.FamilyWindow < s.SiteWindow || s.FamilyWindow > 1000 || s.MinimumSamples < 2 || s.MinimumSamples > s.SiteWindow/2 || s.MinimumLinks < 2 || s.MinimumLinks > 10000 || s.Similarity < .5 || s.Similarity > 1 || s.Redundancy < .5 || s.Redundancy > 1 || s.Novelty < 0 || s.Novelty > .5 {
		return fmt.Errorf("invalid trap detection windows or thresholds")
	}
	return nil
}

type Observation struct {
	CrossSiteLinks float64    `json:"cross_site_links"`
	Targets        HashVector `json:"targets"`
	URI            string     `json:"uri"`
	Site           string     `json:"site"`
	Signature      HashVector `json:"signature"`
	Patterns       HashVector `json:"patterns"`
	Destinations   []string   `json:"destinations"`
	Links          int        `json:"links"`
	Tokens         int        `json:"tokens"`
}

func hash(s string) uint64 { h := fnv.New64a(); _, _ = h.Write([]byte(s)); return h.Sum64() }
func mix(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	return x ^ (x >> 31)
}
func Similarity(a, b []uint64) float64 {
	if len(a) != 64 || len(b) != 64 {
		return 0
	}
	same := 0
	for i := range a {
		if a[i] == b[i] {
			same++
		}
	}
	return float64(same) / 64
}
func Pattern(raw string) uint64 {
	u, e := url.Parse(raw)
	if e != nil {
		return 0
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, p := range segments {
		if strings.ContainsAny(p, "0123456789") {
			segments[i] = "{id}"
		} else if len(p) > 24 {
			segments[i] = "{slug}"
		}
	}
	keys := []string{}
	for k := range u.Query() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return hash(strings.Join(segments, "/") + "?" + strings.Join(keys, "&"))
}

const ObservationByteLimit = 16 << 20

// Structure is private to a delivery. It retains bounded hashes, never page text.
type Structure struct {
	stack            []string
	overflow, tokens int
	shingles         map[uint64]bool
	done             bool
}

func NewStructure() *Structure { return &Structure{shingles: map[uint64]bool{}} }
func (s *Structure) Add(kind html.TokenType, token html.Token) {
	if s.done || s.tokens >= 100000 {
		return
	}
	if kind != html.StartTagToken && kind != html.EndTagToken && kind != html.SelfClosingTagToken {
		return
	}
	if token.Data == "script" || token.Data == "style" {
		return
	}
	s.tokens++
	if kind == html.EndTagToken {
		if s.overflow > 0 {
			s.overflow--
			return
		}
		for i := len(s.stack) - 1; i >= 0; i-- {
			if s.stack[i] == token.Data {
				s.stack = s.stack[:i]
				break
			}
		}
		return
	}
	path := strings.Join(s.stack, "/") + "/" + token.Data
	s.shingles[hash(path)] = true
	// Ancestry shingles retain local structure without encoding sibling order.
	// Moving a complete section therefore does not change its fingerprint.
	start := max(0, len(s.stack)-3)
	s.shingles[hash(strings.Join(s.stack[start:], ">")+">"+token.Data)] = true
	if len(s.shingles) > 4096 {
		s.done = true
		return
	}
	if kind == html.StartTagToken && !voidTag(token.Data) {
		if len(s.stack) < 64 && s.overflow == 0 {
			s.stack = append(s.stack, token.Data)
		} else {
			s.overflow++
		}
	}
}
func (s *Structure) Observation(raw string, links []string) Observation {
	o := Observation{URI: raw, Site: site.Key(raw), Signature: make([]uint64, 64), Patterns: []uint64{}, Destinations: []string{}}
	for i := range o.Signature {
		o.Signature[i] = ^uint64(0)
	}

	o.Tokens = s.tokens
	for h := range s.shingles {
		for i := range o.Signature {
			v := mix(h + uint64(i)*0x9e3779b97f4a7c15)
			if v < o.Signature[i] {
				o.Signature[i] = v
			}
		}
	}
	targets := map[uint64]bool{}
	patterns := map[uint64]bool{}
	dest := map[string]bool{}
	crossSite := 0
	for _, u := range links {
		if o.Links >= 10000 {
			break
		}
		if _, e := url.Parse(u); e != nil {
			continue
		}
		o.Links++
		if h := hash(u); !targets[h] && len(o.Targets) < 128 {
			targets[h] = true
			o.Targets = append(o.Targets, h)
		}
		patterns[Pattern(u)] = true
		key := site.Key(u)
		if key != o.Site {
			crossSite++
			if !dest[key] && len(o.Destinations) < 128 {
				dest[key] = true
				o.Destinations = append(o.Destinations, key)
			}
		}
	}
	o.CrossSiteLinks = float64(crossSite) / float64(max(1, o.Links))
	for p := range patterns {
		o.Patterns = append(o.Patterns, p)
	}
	sort.Slice(o.Patterns, func(i, j int) bool { return o.Patterns[i] < o.Patterns[j] })
	if len(o.Patterns) > 128 {
		o.Patterns = o.Patterns[:128]
	}

	return o
}

func Observe(ctx context.Context, r io.Reader, raw string, links []string) (Observation, error) {
	s := NewStructure()
	z := html.NewTokenizer(io.LimitReader(r, ObservationByteLimit))
	for !s.done && s.tokens < 100000 {
		if e := ctx.Err(); e != nil {
			return Observation{}, e
		}
		kind := z.Next()
		if kind == html.ErrorToken {
			if e := z.Err(); e != io.EOF {
				return Observation{}, e
			}
			break
		}
		if kind == html.StartTagToken || kind == html.EndTagToken || kind == html.SelfClosingTagToken {
			s.Add(kind, z.Token())
		}
	}
	return s.Observation(raw, links), nil
}

func voidTag(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}
