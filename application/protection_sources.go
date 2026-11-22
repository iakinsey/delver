package application

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/protection"
)

func purgeDefinitionSources(d *CrawlDefinition, match func(string) bool) {
	filter := func(values []string) []string {
		out := []string{}
		for _, v := range values {
			if !match(v) {
				out = append(out, v)
			}
		}
		return out
	}
	d.Seeds = filter(d.Seeds)
	d.Feeds = filter(d.Feeds)
}

// Configuration is not crawl content. Only source declarations are removed;
// credentials, proxy endpoints, names and opaque unrelated integers remain.
func purgeApplicationSources(a *config.Application, match func(string) bool) error {
	for n, w := range a.Workers {
		switch w.Type {
		case "fixed_seed_publisher", "rss_feed_publisher":
			var fields map[string]json.RawMessage
			if e := json.Unmarshal(w.Parameters, &fields); e != nil {
				return e
			}
			var uris []string
			if e := json.Unmarshal(fields["uris"], &uris); e != nil {
				return e
			}
			kept := []string{}
			for _, u := range uris {
				if !match(u) {
					kept = append(kept, u)
				}
			}
			raw, e := json.Marshal(kept)
			if e != nil {
				return e
			}
			fields["uris"] = raw
			a.Workers[n].Parameters, e = json.Marshal(fields)
			if e != nil {
				return e
			}

		default:
			registryMutex.RLock()
			clean := workerProtectionCleaners[w.Type]
			registryMutex.RUnlock()
			if clean != nil {
				next, err := clean(w.Parameters, match)
				if err != nil {
					return err
				}
				if !json.Valid(next) {
					return fmt.Errorf("invalid custom protection configuration")
				}
				a.Workers[n].Parameters = next
			}
		}
	}
	for n, r := range a.Resources {
		registryMutex.RLock()
		clean := resourceProtectionCleaners[r.Type]
		registryMutex.RUnlock()
		if clean != nil {
			next, err := clean(r.Parameters, match)
			if err != nil {
				return err
			}
			if !json.Valid(next) {
				return fmt.Errorf("invalid custom protection configuration")
			}
			a.Resources[n].Parameters = next
		}
	}
	return nil
}
func purgeRecordSources(r *CrawlRecord, match func(string) bool) error {
	for _, op := range r.Operations {
		if protection.References(op.Error, match) {
			op.Error = ""
		}
		if protection.References(op.Record, match) {
			op.Record = ""
		} else if decoded, e := base64.RawURLEncoding.DecodeString(op.Record); e == nil && protection.References(string(decoded), match) {
			op.Record = ""
		}
	}

	purgeDefinitionSources(&r.Definition, match)
	if r.Draft != nil {
		purgeDefinitionSources(r.Draft, match)
	}
	for _, a := range []*config.Application{r.Expert, r.ExpertDraft} {
		if a != nil {
			if e := purgeApplicationSources(a, match); e != nil {
				return e
			}
		}
	}
	for n := range r.Runs {
		run := &r.Runs[n]
		if run.Summary != nil {
			for key, activity := range run.Summary.Workers {
				if protection.References(activity.LastError, match) {
					activity.LastError = ""
					run.Summary.Workers[key] = activity
				}
			}
			for key, health := range run.Summary.Health {
				if protection.References(health, match) {
					delete(run.Summary.Health, key)
				}
			}
		}

		purgeDefinitionSources(&run.Definition, match)
		if run.Expert != nil {
			if e := purgeApplicationSources(run.Expert, match); e != nil {
				return e
			}
		}
		if len(run.ConfigurationSnapshot) > 0 {
			next, e := purgeConfigurationJSON(run.ConfigurationSnapshot, match)
			if e != nil {
				return e
			}
			run.ConfigurationSnapshot = next
		}

	}
	return nil
}

func (w *Workspace) saveProtectedRecordLocked(r *CrawlRecord) error {
	d := r.Definition
	if r.Draft != nil {
		d = *r.Draft
	}
	uris := append(append([]string{}, d.Seeds...), d.Feeds...)
	a := r.Expert
	if r.ExpertDraft != nil {
		a = r.ExpertDraft
	}
	if a != nil {
		blocked := false
		match := func(raw string) bool {
			if _, e := protection.NormalizeDestination(raw); e != nil {
				return false
			}
			if len(w.protection.Match(raw)) > 0 {
				blocked = true
				return true
			}
			return false
		}
		for _, worker := range a.Workers {
			registryMutex.RLock()
			clean := workerProtectionCleaners[worker.Type]
			registryMutex.RUnlock()
			if clean != nil {
				if _, e := clean(worker.Parameters, match); e != nil {
					return e
				}
			}
		}
		for _, resource := range a.Resources {
			registryMutex.RLock()
			clean := resourceProtectionCleaners[resource.Type]
			registryMutex.RUnlock()
			if clean != nil {
				if _, e := clean(resource.Parameters, match); e != nil {
					return e
				}
			}
		}
		if blocked {
			return protection.ErrBlocked
		}
		for _, worker := range a.Workers {
			if worker.Type == "fixed_seed_publisher" || worker.Type == "rss_feed_publisher" {
				var p struct {
					URIs []string `json:"uris"`
				}
				if e := json.Unmarshal(worker.Parameters, &p); e != nil {
					return e
				}
				uris = append(uris, p.URIs...)
			}
		}
	}
	return w.protection.GuardMany(uris, w.saveLocked)
}
