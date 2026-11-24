package application

import (
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/frontier"
)

// Opt-in custom pipelines must deliver the same protected features consumed by
// the built-in accumulator. Absence of settings keeps custom pipelines intact.
func validateRepeatedPages(app config.Application) error {
	frontierCount := 0
	enabled := false
	for _, resource := range app.Resources {
		if resource.Type != "frontier_store" {
			continue
		}
		frontierCount++
		var p frontier.StoreParams
		if err := json.Unmarshal(resource.Parameters, &p); err != nil {
			return err
		}
		enabled = enabled || p.RepeatedPages != nil && p.RepeatedPages.Mode != "off"
	}
	if enabled && frontierCount != 1 {
		return fmt.Errorf("repeated pages requires one authoritative frontier per crawl")
	}

	for _, resource := range app.Resources {
		if resource.Type != "frontier_store" {
			continue
		}
		var p frontier.StoreParams
		if err := json.Unmarshal(resource.Parameters, &p); err != nil {
			return err
		}
		if p.RepeatedPages == nil || p.RepeatedPages.Mode == "off" {
			continue
		}
		if err := p.RepeatedPages.Validate(); err != nil {
			return err
		}
		supported := false
		for _, accumulator := range app.Workers {
			if accumulator.Type != "frontier_accumulator" {
				continue
			}
			var refs struct {
				Store string `json:"frontier_store"`
			}
			if err := json.Unmarshal(accumulator.Parameters, &refs); err != nil {
				return err
			}
			if refs.Store != resource.Name {
				continue
			}
			for _, extractor := range app.Workers {
				if extractor.Type != "composite_extractor" || extractor.Outbox != accumulator.Inbox {
					continue
				}
				var ep struct {
					Enabled []string `json:"enabled"`
				}
				if err := json.Unmarshal(extractor.Parameters, &ep); err != nil {
					return err
				}
				enabled := map[string]bool{}
				for _, name := range ep.Enabled {
					enabled[name] = true
				}
				supported = supported || enabled["url"] && enabled["text"] && enabled["adversarial"]
			}
		}
		if !supported {
			return fmt.Errorf("repeated pages requires a connected built-in composite extractor (url, text, adversarial) and frontier accumulator for %s", resource.Name)
		}
	}
	return nil
}
