package application

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/repetition"
	"io"
)

// Patch only named repeated-page settings. UseNumber preserves opaque custom
// parameters, including integers larger than JavaScript's exact range.
func patchRepeatedPages(raw json.RawMessage) (interface{}, error) {
	var request struct {
		ConfigurationJSON string                         `json:"configuration_json"`
		Settings          map[string]repetition.Settings `json:"settings"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewBufferString(request.ConfigurationJSON))
	decoder.UseNumber()
	var document map[string]interface{}
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("configuration must contain one JSON object")
	}
	if document == nil {
		return nil, fmt.Errorf("pipeline must be an object")
	}
	resources, _ := document["resources"].([]interface{})
	found := map[string]bool{}
	for _, entry := range resources {
		resource, ok := entry.(map[string]interface{})
		if !ok || resource["type"] != "frontier_store" {
			continue
		}
		name, _ := resource["name"].(string)
		budget, requested := request.Settings[name]
		if !requested {
			continue
		}
		if err := budget.Validate(); err != nil {
			return nil, err
		}
		params, _ := resource["parameters"].(map[string]interface{})
		if params == nil {
			params = map[string]interface{}{}
			resource["parameters"] = params
		}
		params["repeated_pages"] = budget
		found[name] = true
	}
	for name := range request.Settings {
		if !found[name] {
			return nil, fmt.Errorf("frontier %q not found", name)
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
