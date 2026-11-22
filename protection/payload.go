package protection

import (
	"encoding/json"
	"errors"
)

func PayloadURLs(raw json.RawMessage) []string {
	var p struct {
		URI       string `json:"uri"`
		Effective string `json:"effective_uri"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return nil
	}
	out := []string{}
	for _, s := range []string{p.URI, p.Effective} {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
func (p *Policy) CheckPayload(raw json.RawMessage) error {
	if p == nil {
		return nil
	}
	if e := p.HealthError(); e != nil {
		return e
	}
	for _, u := range PayloadURLs(raw) {
		if e := p.Check(u); e != nil {
			return e
		}
	}
	return nil
}
func (p *Policy) GuardPayload(raw json.RawMessage, fn func() error) error {
	e := p.GuardMany(PayloadURLs(raw), fn)
	if errors.Is(e, ErrBlocked) {
		return nil
	}
	return e
}

func (p *Policy) SanitizePayload(raw json.RawMessage) (json.RawMessage, error) {
	if p == nil {
		return raw, nil
	}
	index := p.current.Load()
	if index == nil || len(index.domains)+len(index.urls)+len(index.quarantines) == 0 {
		return raw, nil
	}
	match := func(uri string) bool {
		if _, e := NormalizeDestination(uri); e != nil {
			return false
		}
		return len(matchIndex(uri, index)) > 0
	}
	clean, _, e := SanitizeJSON(raw, match)
	return clean, e
}
