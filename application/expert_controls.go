package application

import (
	"bytes"
	"encoding/json"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/types/errs"
	"time"
)

func validateTermination(t Termination) error {
	if t.MaxRequests < 0 {
		return errs.NewRequestError("request budget cannot be negative")
	}
	if t.Duration != "" {
		d, err := time.ParseDuration(t.Duration)
		if err != nil || d <= 0 {
			return errs.NewRequestError("running-time limit must be positive")
		}
	}
	return nil
}
func expertRuntimeControls(a config.Application, t Termination) (config.Application, error) {
	if err := validateTermination(t); err != nil {
		return a, err
	}
	frontiers := 0
	fetchInboxes := map[string]bool{}
	for _, w := range a.Workers {
		if w.Type == "http_fetcher" {
			fetchInboxes[w.Inbox] = true
		}
		if t.WhenDrained && w.Manager == "job" && w.Type != "frontier_publisher" && w.Type != "frontier_cleanup" && !w.Once {
			return a, errs.NewRequestError("finish-on-drain requires custom source jobs to declare once:true and use source_progress")
		}
	}
	for i, r := range a.Resources {
		if r.Type != "frontier_store" {
			continue
		}
		frontiers++
		var p map[string]interface{}
		decoder := json.NewDecoder(bytes.NewReader(r.Parameters))
		decoder.UseNumber()
		if err := decoder.Decode(&p); err != nil {
			return a, err
		}
		if t.MaxRequests > 0 {
			p["max_dispatches"] = t.MaxRequests
		}
		if t.WhenDrained {
			p["revisit_after"] = "never"
		}
		data, _ := json.Marshal(p)
		a.Resources[i].Parameters = data
	}
	if t.MaxRequests > 0 {
		if frontiers != 1 {
			return a, errs.NewRequestError("a workspace request budget requires exactly one frontier_store")
		}
		for _, w := range a.Workers {
			if fetchInboxes[w.Outbox] {
				return a, errs.NewRequestError("route source requests through frontier_admission to enforce the request budget")
			}
		}
	}
	return a, Validate(a)
}
