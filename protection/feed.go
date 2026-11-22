package protection

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Preview struct {
	List    List `json:"list"`
	Added   int  `json:"added"`
	Removed int  `json:"removed"`
}

func PreviewList(ctx context.Context, client *http.Client, l List, old *List) (Preview, error) {
	if l.Refresh == "" {
		l.Refresh = "6h"
	}
	var text = l.Text
	if l.URL != "" {
		if _, e := NormalizeDestination(l.URL); e != nil {
			return Preview{}, e
		}
		req, e := http.NewRequestWithContext(ctx, "GET", l.URL, nil)
		if e != nil {
			return Preview{}, e
		}
		res, e := client.Do(req)
		if e != nil {
			return Preview{}, e
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return Preview{}, fmt.Errorf("feed returned HTTP %d", res.StatusCode)
		}
		raw, e := io.ReadAll(io.LimitReader(res.Body, (64<<20)+1))
		if e != nil {
			return Preview{}, e
		}
		if len(raw) > 64<<20 {
			return Preview{}, fmt.Errorf("feed exceeds 64 MiB")
		}
		text = string(raw)
	}
	entries, e := ParseList(text)
	if e != nil {
		return Preview{}, e
	}
	if len(entries) == 0 {
		return Preview{}, fmt.Errorf("list contains no entries")
	}
	l.Entries = entries
	l.Version = entryVersion(entries)
	l.Updated = time.Now().UTC()
	l.Error = ""
	if l.URL != "" {
		l.Text = ""
	}
	prev := map[Entry]bool{}
	if old != nil {
		for _, v := range old.Entries {
			prev[v] = true
		}
	}
	next := map[Entry]bool{}
	p := Preview{List: l}
	for _, v := range entries {
		next[v] = true
		if !prev[v] {
			p.Added++
		}
	}
	for v := range prev {
		if !next[v] {
			p.Removed++
		}
	}
	return p, nil
}

func (p *Policy) RecordFeedError(id, source, reason string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for n, l := range p.state.Lists {
		if l.ID == id && l.URL == source {
			p.state.Lists[n].Error = reason
			p.state.Lists[n].Attempted = time.Now().UTC()
			return p.persistLocked()
		}
	}
	return nil
}
func (p *Policy) DueFeeds(now time.Time) []List {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := []List{}
	for _, l := range p.state.Lists {
		interval, e := time.ParseDuration(l.Refresh)
		if e != nil || !l.Enabled || l.URL == "" {
			continue
		}
		at := l.Updated
		if l.Attempted.After(at) {
			at = l.Attempted
		}
		if now.Sub(at) >= interval {
			out = append(out, l)
		}
	}
	return out
}
