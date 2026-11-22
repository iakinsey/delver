package protection

import (
	"net/url"
	"strings"
)

// Matcher compiles lists once; work per candidate depends on hostname length,
// rather than on the number of subscribed entries.
type Matcher struct {
	domains map[string]bool
	urls    map[string]bool
}

func NewMatcher(entries []Entry, targets []Target) *Matcher {
	m := &Matcher{map[string]bool{}, map[string]bool{}}
	for _, e := range entries {
		if e.Domain {
			m.domains[e.Value] = true
		} else {
			m.urls[e.Value] = true
		}
	}
	for _, t := range targets {
		m.domains[t.Site] = true
	}
	return m
}
func (m *Matcher) MatchesDomain(host string) bool {
	for h := strings.TrimSuffix(strings.ToLower(host), "."); h != ""; {
		if m.domains[h] {
			return true
		}
		n := strings.IndexByte(h, '.')
		if n < 0 {
			break
		}
		h = h[n+1:]
	}
	return false
}
func (m *Matcher) Matches(raw string) bool {
	normal, e := NormalizeDestination(raw)
	if e != nil {
		return false
	}
	if m.urls[normal] {
		return true
	}
	u, _ := url.Parse(normal)
	return m.MatchesDomain(u.Hostname())
}
