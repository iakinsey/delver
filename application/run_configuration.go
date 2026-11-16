package application

import (
	"encoding/json"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/flow"
)

// Preserve historical input verbatim when current policy decoding removes retired fields.
func (r *CrawlRun) UnmarshalJSON(data []byte) error {
	type plain CrawlRun
	value := plain{Definition: DefaultCrawlDefinition()}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if len(value.ConfigurationSnapshot) == 0 {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		snapshot := map[string]json.RawMessage{}
		if definition, ok := raw["definition"]; ok {
			snapshot["definition"] = definition
		}
		if pipeline, ok := raw["expert"]; ok && string(pipeline) != "null" {
			snapshot["pipeline"] = pipeline
		}
		if len(snapshot) > 0 {
			value.ConfigurationSnapshot, _ = json.Marshal(snapshot)
		}
	}
	*r = CrawlRun(value)
	return nil
}
func configurationSnapshot(definition CrawlDefinition, pipeline *config.Application, execution flow.ExecutionLimits) json.RawMessage {
	data, _ := json.Marshal(struct {
		Definition CrawlDefinition      `json:"definition"`
		Pipeline   *config.Application  `json:"pipeline,omitempty"`
		Execution  flow.ExecutionLimits `json:"execution"`
	}{definition, pipeline, execution})
	return data
}
