package protection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/iakinsey/delver/site"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/text/unicode/norm"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrUnavailable = errors.New("protection persistence unavailable")

var ErrChanged = errors.New("protection policy changed; retry operation")
var ErrBlocked = errors.New("destination blocked by workspace protection")

type Target struct {
	Reason   string `json:"reason,omitempty"`
	Site     string `json:"site"`
	Distance int    `json:"distance"`
}
type Incident struct {
	SourceName    string            `json:"source_name,omitempty"`
	SourceVersion string            `json:"source_version,omitempty"`
	TargetCount   int               `json:"target_count"`
	Entries       []Entry           `json:"entries,omitempty"`
	ID            string            `json:"id"`
	CrawlID       string            `json:"crawl_id"`
	RunID         string            `json:"run_id"`
	URL           string            `json:"url"`
	RuleID        string            `json:"rule_id"`
	Kind          string            `json:"kind"`
	Created       time.Time         `json:"created"`
	Depth         int               `json:"depth"`
	Revision      int64             `json:"graph_revision"`
	Partial       bool              `json:"partial"`
	Targets       []Target          `json:"targets"`
	State         string            `json:"state"`
	Error         string            `json:"error,omitempty"`
	Progress      map[string]string `json:"progress"`
}
type State struct {
	Health        string     `json:"health,omitempty"`
	InspectionMiB int        `json:"inspection_memory_mib"`
	Revision      uint64     `json:"revision"`
	Lists         []List     `json:"lists"`
	Rules         []Rule     `json:"rules"`
	Incidents     []Incident `json:"incidents"`
}
type flight struct {
	uri    string
	cancel context.CancelFunc
}
type policyIndex struct {
	domains, urls map[string][]string
	quarantines   map[string]string
	unavailable   bool
}

var ErrCancelled = errors.New("protection cleanup cancelled")

type Policy struct {
	purgeCancels  map[string]context.CancelFunc
	writeSequence uint64
	writes        map[uint64]struct{}
	writesChanged chan struct{}
	current       atomic.Pointer[policyIndex]
	persistedRows map[string]string
	listVersions  map[string]string
	jobEntries    map[string]bool
	storeError    error
	healthChanged chan struct{}
	memoryUsed    int64
	memoryChanged chan struct{}
	mu            sync.RWMutex
	db            *sql.DB
	state         State
	domains       map[string][]string
	urls          map[string][]string
	quarantines   map[string]string
	flights       map[string]flight
}

func Open(path string) (*Policy, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite3", "file:"+path+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=FULL")
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	p := &Policy{db: db, state: State{InspectionMiB: 256, Revision: 1, Lists: []List{}, Rules: DefaultRules(), Incidents: []Incident{}}, flights: map[string]flight{}, healthChanged: make(chan struct{}), writes: map[uint64]struct{}{}, purgeCancels: map[string]context.CancelFunc{}, writesChanged: make(chan struct{}), memoryChanged: make(chan struct{}), persistedRows: map[string]string{}, listVersions: map[string]string{}, jobEntries: map[string]bool{}}
	if e = p.loadStorage(); e != nil {
		db.Close()
		return nil, e
	}
	if p.state.InspectionMiB == 0 {
		p.state.InspectionMiB = 256
	}
	if e = p.persistLocked(); e != nil {
		db.Close()
		return nil, e
	}
	_ = os.Chmod(path, 0600)
	p.reindexLocked()
	return p, nil
}
func (p *Policy) Close() error { return p.db.Close() }
func (p *Policy) Snapshot() State {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s := p.state
	s.Lists = append([]List{}, s.Lists...)
	for n := range s.Lists {
		s.Lists[n].Entries = append([]Entry{}, s.Lists[n].Entries...)
	}
	s.Rules = append([]Rule{}, s.Rules...)
	for n := range s.Rules {
		s.Rules[n].Phrases = append([]string{}, s.Rules[n].Phrases...)
	}
	s.Incidents = p.incidentsLocked()
	return s
}
func (p *Policy) incidentsLocked() []Incident {
	out := append([]Incident{}, p.state.Incidents...)
	for n := range out {
		out[n].Targets = append([]Target{}, out[n].Targets...)
		out[n].Progress = map[string]string{}
		for k, v := range p.state.Incidents[n].Progress {
			out[n].Progress[k] = v
		}
	}
	return out
}
func (p *Policy) Incidents() []Incident {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.incidentsLocked()
}
func (p *Policy) reindexLocked() {
	p.domains = map[string][]string{}
	p.urls = map[string][]string{}

	for _, l := range p.state.Lists {
		if !l.Enabled || l.Updated.IsZero() {
			continue
		}
		for _, e := range l.Entries {
			m := p.urls
			if e.Domain {
				m = p.domains
			}
			m[e.Value] = append(m[e.Value], l.ID)
		}
	}
	p.reindexQuarantinesLocked()
}
func (p *Policy) reindexQuarantinesLocked() {
	p.quarantines = map[string]string{}
	for _, i := range p.state.Incidents {
		if i.Kind == "restricted" {
			for _, t := range i.Targets {
				p.quarantines[t.Site] = i.ID
			}
		}
	}
	p.current.Store(&policyIndex{p.domains, p.urls, p.quarantines, p.storeError != nil})
}
func (p *Policy) matchLocked(raw string) []string {
	return matchIndex(raw, &policyIndex{p.domains, p.urls, p.quarantines, p.storeError != nil})
}
func matchIndex(raw string, index *policyIndex) []string {
	normal, e := NormalizeDestination(raw)
	if e != nil {
		return []string{"invalid URL"}
	}
	u, _ := url.Parse(normal)
	if id := index.quarantines[site.Key(u.Hostname())]; id != "" {
		return []string{id}
	}
	matches := append([]string{}, index.urls[normal]...)
	for h := u.Hostname(); h != ""; {
		matches = append(matches, index.domains[h]...)
		at := strings.IndexByte(h, '.')
		if at < 0 {
			break
		}
		h = h[at+1:]
	}
	return matches
}
func (p *Policy) Match(raw string) []string {
	if p == nil {
		return nil
	}
	index := p.current.Load()
	if index == nil {
		return []string{"protection unavailable"}
	}
	return matchIndex(raw, index)
}
func (p *Policy) Check(raw string) error {
	if p == nil {
		return nil
	}
	if e := p.HealthError(); e != nil {
		return e
	}
	if len(p.Match(raw)) > 0 {
		return ErrBlocked
	}
	return nil
}

// Guard admits a write before it starts and tracks its completion. Policy
// activation can cancel requests immediately without waiting for a slow sink.
// Cleanup waits for all writes admitted before its fence before deleting data.
func (p *Policy) Guard(raw string, fn func() error) error { return p.GuardMany([]string{raw}, fn) }
func (p *Policy) GuardMany(uris []string, fn func() error) error {
	if p == nil {
		return fn()
	}
	p.mu.Lock()
	if p.storeError != nil {
		p.mu.Unlock()
		return ErrUnavailable
	}
	for _, raw := range uris {
		if raw != "" && len(p.matchLocked(raw)) > 0 {
			p.mu.Unlock()
			return ErrBlocked
		}
	}
	p.writeSequence++
	id := p.writeSequence
	p.writes[id] = struct{}{}
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.writes, id)
		close(p.writesChanged)
		p.writesChanged = make(chan struct{})
		p.mu.Unlock()
	}()
	return fn()
}
func (p *Policy) WaitWrites(ctx context.Context) error {
	p.mu.RLock()
	fence := p.writeSequence
	p.mu.RUnlock()
	for {
		p.mu.RLock()
		busy := false
		for id := range p.writes {
			if id <= fence {
				busy = true
				break
			}
		}
		changed := p.writesChanged
		p.mu.RUnlock()
		if !busy {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}
func (p *Policy) Begin(parent context.Context, raw string) (context.Context, func(), error) {
	ctx, cancel := context.WithCancel(parent)
	if p == nil {
		return ctx, cancel, nil
	}
	p.mu.Lock()
	if p.storeError != nil {
		p.mu.Unlock()
		cancel()
		return ctx, func() {}, ErrUnavailable
	}
	if len(p.matchLocked(raw)) > 0 {
		p.mu.Unlock()
		cancel()
		return ctx, func() {}, ErrBlocked
	}
	id := uuid.NewString()
	p.flights[id] = flight{raw, cancel}
	p.mu.Unlock()
	return ctx, func() { cancel(); p.mu.Lock(); delete(p.flights, id); p.mu.Unlock() }, nil
}
func (p *Policy) cancelLocked() {
	for _, f := range p.flights {
		if p.storeError != nil || len(p.matchLocked(f.uri)) > 0 {
			f.cancel()
		}
	}
}
func (p *Policy) Update(revision uint64, lists []List, rules []Rule) error {
	return p.update(revision, lists, rules, nil)
}
func (p *Policy) Activate(revision uint64, lists []List, rules []Rule, source List) error {
	var job *Incident
	if source.Enabled && !source.Updated.IsZero() {
		job = &Incident{SourceName: source.Name, SourceVersion: source.Version, ID: uuid.NewString(), Kind: "blocklist", RuleID: source.ID, Created: time.Now().UTC(), State: "pending", Progress: map[string]string{"targets": "captured"}, Targets: []Target{}, Entries: append([]Entry{}, source.Entries...)}
	}
	return p.update(revision, lists, rules, job)
}
func (p *Policy) update(revision uint64, lists []List, rules []Rule, job *Incident) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if revision != p.state.Revision {
		return fmt.Errorf("protection revision conflict")
	}
	seen := map[string]bool{}
	for _, l := range lists {
		if l.ID == "" || seen[l.ID] || strings.TrimSpace(l.Name) == "" {
			return fmt.Errorf("unique list ID and name required")
		}
		seen[l.ID] = true
		d, e := time.ParseDuration(l.Refresh)
		if e != nil || d < 5*time.Minute || d > 30*24*time.Hour {
			return fmt.Errorf("refresh must be between 5m and 720h")
		}
		if l.URL != "" {
			if _, e := NormalizeDestination(l.URL); e != nil {
				return e
			}
		}
	}
	seen = map[string]bool{}
	for _, r := range rules {
		if r.ID == "" || seen[r.ID] || len(r.Phrases) == 0 {
			return fmt.Errorf("unique rule ID and phrases required")
		}
		seen[r.ID] = true
		for _, v := range r.Phrases {
			v = strings.TrimSpace(v)
			letters := 0
			for _, r := range normalizedText(v) {
				if unicode.IsLetter(r) || unicode.IsDigit(r) {
					letters++
				}
			}
			if letters < 8 || utf8.RuneCountInString(v) > 1024 {
				return fmt.Errorf("rule phrases must contain 8–1024 characters")
			}
		}
	}
	old := p.state
	for n := range lists {
		if lists[n].Version == "" {
			lists[n].Version = entryVersion(lists[n].Entries)
		}
	}
	p.state.Incidents = append([]Incident{}, p.state.Incidents...)
	enabled := map[string]bool{}
	versions := map[string]string{}
	for _, list := range lists {
		enabled[list.ID] = list.Enabled
		versions[list.ID] = list.Version
	}
	for n, i := range p.state.Incidents {
		if i.Kind == "blocklist" && (!enabled[i.RuleID] || i.SourceVersion != "" && i.SourceVersion != versions[i.RuleID]) && i.State != "completed" && i.State != "cancelled" {
			i.State = "cancelled"
			i.Error = ""
			i.Entries = nil
			p.state.Incidents[n] = i
		}
	}
	p.state.Lists = lists
	if job != nil {
		p.state.Incidents = append(p.state.Incidents, *job)
	}
	p.state.Rules = rules
	p.state.Revision++
	if e := p.persistLocked(); e != nil {
		p.state = old
		return e
	}
	p.reindexLocked()
	for _, i := range p.state.Incidents {
		if i.State == "cancelled" {
			if cancel := p.purgeCancels[i.ID]; cancel != nil {
				cancel()
			}
		}
	}
	p.cancelLocked()
	return nil
}
func (p *Policy) Trigger(raw, rule, crawl, run string, depth int) (Incident, error) {
	p.mu.Lock()
	if id := p.quarantines[site.Key(raw)]; id != "" {
		for _, i := range p.state.Incidents {
			if i.ID == id {
				p.mu.Unlock()
				return i, ErrBlocked
			}
		}
	}
	sourceName := ""
	for _, r := range p.state.Rules {
		if r.ID == rule {
			sourceName = r.Name
			break
		}
	}
	location, _ := NormalizeDestination(raw)
	i := Incident{SourceName: sourceName, ID: uuid.NewString(), CrawlID: crawl, RunID: run, URL: location, RuleID: rule, Kind: "restricted", Created: time.Now().UTC(), Depth: depth, Targets: []Target{{Site: site.Key(raw), Reason: "detected_site"}}, State: "pending", Progress: map[string]string{}}
	p.state.Incidents = append(p.state.Incidents, copyIncident(i, 0, len(i.Targets)))
	// Keep the in-memory deny even on disk failure, and return the persistence error.
	e := p.persistLocked()
	p.reindexQuarantinesLocked()
	p.cancelLocked()
	p.mu.Unlock()
	if e != nil {
		return i, e
	}
	return i, ErrBlocked
}
func (p *Policy) SetIncident(i Incident) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for n, v := range p.state.Incidents {
		if v.ID == i.ID {
			if v.State == "cancelled" {
				return ErrCancelled
			}
			// Captured propagation targets are immutable. Metadata updates and
			// retries cannot replace them with a graph recalculated after deletion.
			if v.Progress["targets"] == "captured" {
				i.Targets = v.Targets
				i.Revision = v.Revision
				i.Partial = v.Partial
				i.Depth = v.Depth
			}
			progress := map[string]string{}
			for key, value := range i.Progress {
				progress[key] = value
			}
			i.Progress = progress
			changed := v.Kind != i.Kind || len(v.Targets) != len(i.Targets)
			sameTargets := len(i.Targets) == 0 || len(v.Targets) > 0 && &i.Targets[0] == &v.Targets[0]
			if !changed && !sameTargets {
				for n := range i.Targets {
					if i.Targets[n] != v.Targets[n] {
						changed = true
						break
					}
				}
			}
			if changed {
				i.Targets = append([]Target{}, i.Targets...)
			}
			p.state.Incidents[n] = i
			e := p.persistLocked()
			if e != nil && i.State == "completed" {
				p.state.Incidents[n] = v
			}
			if changed {
				p.reindexQuarantinesLocked()
				p.cancelLocked()
			}
			return e
		}
	}
	return fmt.Errorf("incident not found")
}

var tags = regexp.MustCompile(`<[^>]*>`)

func normalizedText(s string) string {
	s = tags.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = strings.ToLower(norm.NFKC.String(s))
	return normalizedWhitespace(s)
}

// normalizedWhitespace is equivalent to Join(Fields(s), " ") without a slice
// of every word. Ordinary single-spaced text needs no additional allocation.
func normalizedWhitespace(s string) string {
	s = strings.TrimSpace(s)
	previousSpace := false
	for _, r := range s {
		space := false
		if r < utf8.RuneSelf {
			space = r == ' ' || r >= '\t' && r <= '\r'
		} else {
			space = unicode.IsSpace(r)
		}
		if space && (r != ' ' || previousSpace) {
			var out strings.Builder
			out.Grow(len(s))
			start := -1
			for at, value := range s {
				space := false
				if value < utf8.RuneSelf {
					space = value == ' ' || value >= '\t' && value <= '\r'
				} else {
					space = unicode.IsSpace(value)
				}
				if space {
					if start >= 0 {
						if out.Len() > 0 {
							out.WriteByte(' ')
						}
						out.WriteString(s[start:at])
						start = -1
					}
				} else if start < 0 {
					start = at
				}
			}
			if start >= 0 {
				if out.Len() > 0 {
					out.WriteByte(' ')
				}
				out.WriteString(s[start:])
			}
			return out.String()
		}
		previousSpace = space
	}
	return s
}
func (p *Policy) Inspect(raw string, body []byte, crawl, run string, depth int) error {
	if p == nil {
		return nil
	}
	if e := p.Check(raw); e != nil {
		return e
	}
	text := normalizedText(string(body))
	u, _ := url.PathUnescape(raw)
	text += " " + normalizedText(u)
	p.mu.RLock()
	rules := append([]Rule(nil), p.state.Rules...)
	p.mu.RUnlock()
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		for _, phrase := range r.Phrases {
			if phraseMatches(text, normalizedText(phrase)) {
				_, e := p.Trigger(raw, r.ID, crawl, run, depth)
				return e
			}
		}
	}
	return nil
}

func phraseMatches(text, phrase string) bool {
	if phrase == "" {
		return false
	}
	for len(text) >= len(phrase) {
		at := strings.Index(text, phrase)
		if at < 0 {
			return false
		}
		end := at + len(phrase)
		before, after := false, false
		if at > 0 {
			r, _ := utf8.DecodeLastRuneInString(text[:at])
			before = unicode.IsLetter(r) || unicode.IsDigit(r)
		}
		if end < len(text) {
			r, _ := utf8.DecodeRuneInString(text[end:])
			after = unicode.IsLetter(r) || unicode.IsDigit(r)
		}
		if !before && !after {
			return true
		}
		text = text[at+1:]
	}
	return false
}
