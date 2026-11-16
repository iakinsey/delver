package application

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/frontier"
)

// patchFetch edits only requested runtime settings while preserving opaque JSON
// numbers and custom component parameters in expert pipelines.
func patchFetch(raw json.RawMessage) (interface{}, error) {
	var request struct {
		ConfigurationJSON string      `json:"configuration_json"`
		Fetch             flow.Policy `json:"fetch"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	if err := request.Fetch.Validate(); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewBufferString(request.ConfigurationJSON))
	decoder.UseNumber()
	var document map[string]interface{}
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if document == nil {
		return nil, fmt.Errorf("pipeline must be an object")
	}
	configuration, _ := document["config"].(map[string]interface{})
	if configuration == nil {
		configuration = map[string]interface{}{}
		document["config"] = configuration
	}
	data, _ := json.Marshal(request.Fetch)
	var fetch interface{}
	json.Unmarshal(data, &fetch)
	configuration["fetch"] = fetch
	workers, _ := document["workers"].([]interface{})
	found := false
	inboxes := map[string]bool{}
	for _, entry := range workers {
		if w, ok := entry.(map[string]interface{}); ok && w["type"] == "http_fetcher" {
			w["count"] = request.Fetch.PoolSize()
			if inbox, ok := w["inbox"].(string); ok {
				inboxes[inbox] = true
			}
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("fetch execution controls require a built-in HTTP fetcher")
	}

	original, err := Decode(bytes.NewBufferString(request.ConfigurationJSON))
	if err != nil {
		return nil, err
	}
	resources, _ := document["resources"].([]interface{})
	for _, entry := range resources {
		if resource, ok := entry.(map[string]interface{}); ok && resource["type"] == "crawl_queue" {
			if name, ok := resource["name"].(string); ok && inboxes[name] {
				params, _ := resource["parameters"].(map[string]interface{})
				if params == nil {
					params = map[string]interface{}{}
					resource["parameters"] = params
				}
				params["accept_frontier_generations"] = true
			}
		}
		if resource, ok := entry.(map[string]interface{}); ok && resource["type"] == "frontier_store" {
			params, _ := resource["parameters"].(map[string]interface{})
			if params == nil {
				params = map[string]interface{}{}
				resource["parameters"] = params
			}
			p := request.Fetch
			http := original.Config.HTTPClient
			params["retry"] = &frontier.RetryPolicy{Share: p.RetryShare, Limit: http.MaxRetries, Base: flow.Duration(http.RetryDelay), Maximum: flow.Duration(http.RetryMaxDelay), FailureThreshold: p.FailureThreshold, HostCooldown: p.HostCooldown, HostCooldownMax: p.HostCooldownMax, ProbeInterval: p.RecoveryProbeInterval}
		}
	}
	updated, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	if _, err = Decode(bytes.NewReader(updated)); err != nil {
		return nil, err
	}
	return map[string]interface{}{"configuration_json": string(updated)}, nil
}
