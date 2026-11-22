package protection

import (
	"fmt"
	"strings"
)

func copyIncident(i Incident, offset, limit int) Incident {
	i.Entries = nil
	i.TargetCount = len(i.Targets)
	if offset > len(i.Targets) {
		offset = len(i.Targets)
	}
	end := min(len(i.Targets), offset+limit)
	i.Targets = append([]Target{}, i.Targets[offset:end]...)
	progress := map[string]string{}
	for k, v := range i.Progress {
		progress[k] = v
	}
	i.Progress = progress
	return i
}
func (p *Policy) PublicSnapshot() State {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s := p.state
	if p.storeError != nil {
		s.Health = ErrUnavailable.Error()
	}
	s.Lists = append([]List{}, s.Lists...)
	for n := range s.Lists {
		s.Lists[n].EntryCount = len(s.Lists[n].Entries)
		s.Lists[n].Entries = nil
		s.Lists[n].Text = ""
	}
	s.Rules = append([]Rule{}, s.Rules...)
	s.Incidents = []Incident{}
	for n := len(p.state.Incidents) - 1; n >= 0 && len(s.Incidents) < 100; n-- {
		s.Incidents = append(s.Incidents, copyIncident(p.state.Incidents[n], 0, 0))
	}
	return s
}
func (p *Policy) Incident(id string, offset, limit int) (Incident, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if offset < 0 || limit < 1 {
		return Incident{}, fmt.Errorf("invalid incident page")
	}
	for _, i := range p.state.Incidents {
		if i.ID == id {
			return copyIncident(i, offset, limit), nil
		}
	}
	return Incident{}, fmt.Errorf("incident not found")
}
func (p *Policy) SearchIncidents(query string) []Incident {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := []Incident{}
	query = strings.ToLower(query)
	for n := len(p.state.Incidents) - 1; n >= 0 && len(out) < 100; n-- {
		i := p.state.Incidents[n]
		if strings.Contains(strings.ToLower(i.URL+" "+i.ID+" "+i.SourceName+" "+i.Kind+" "+i.RuleID+" "+i.State), query) {
			out = append(out, copyIncident(i, 0, 0))
		}
	}
	return out
}

func (p *Policy) HasUnfinished() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, i := range p.state.Incidents {
		if i.State != "completed" && i.State != "cancelled" {
			return true
		}
	}
	return false
}
func (p *Policy) Pending() []Incident {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := []Incident{}
	for _, i := range p.state.Incidents {
		if i.State == "pending" || i.State == "running" {
			v := copyIncident(i, 0, len(i.Targets))
			v.Entries = i.Entries
			out = append(out, v)
		}
	}
	return out
}
func (p *Policy) PrivateIncident(id string) (Incident, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, i := range p.state.Incidents {
		if i.ID == id {
			out := copyIncident(i, 0, len(i.Targets))
			out.Entries = i.Entries
			return out, nil
		}
	}
	return Incident{}, fmt.Errorf("incident not found")
}
func (p *Policy) Configuration() State {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s := p.state
	s.Incidents = nil
	s.Lists = append([]List{}, s.Lists...)
	s.Rules = append([]Rule{}, s.Rules...)
	return s
}

func (p *Policy) NeedsGraph(crawlID string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, i := range p.state.Incidents {
		if i.CrawlID == crawlID && i.Kind == "restricted" && i.Progress["targets"] != "captured" {
			return true
		}
	}
	return false
}
