package application

import (
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/protection"
)

// Edit only declared crawl source fields. RawMessage preserves credentials,
// unknown historical controls and large integers throughout old snapshots.
func purgeConfigurationJSON(raw json.RawMessage, match func(string) bool) (json.RawMessage, error) {
	if string(raw) == "null" || len(raw) == 0 {
		return raw, nil
	}
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(raw, &fields); e != nil {
		return nil, e
	}
	for key, value := range fields {
		switch key {
		case "seeds", "feeds":
			var values []string
			if e := json.Unmarshal(value, &values); e != nil {
				return nil, e
			}
			kept := []string{}
			for _, uri := range values {
				if !match(uri) {
					kept = append(kept, uri)
				}
			}
			if len(kept) != len(values) {
				fields[key], _ = json.Marshal(kept)
			}
		case "workers", "resources":
			var declarations []map[string]json.RawMessage
			if e := json.Unmarshal(value, &declarations); e != nil {
				return nil, e
			}
			for _, declaration := range declarations {
				var kind string
				if e := json.Unmarshal(declaration["type"], &kind); e != nil {
					return nil, e
				}
				params := declaration["parameters"]
				if key == "workers" && (kind == "fixed_seed_publisher" || kind == "rss_feed_publisher") {
					var p map[string]json.RawMessage
					if e := json.Unmarshal(params, &p); e != nil {
						return nil, e
					}
					var uris []string
					if e := json.Unmarshal(p["uris"], &uris); e != nil {
						return nil, e
					}
					kept := []string{}
					for _, uri := range uris {
						if !match(uri) {
							kept = append(kept, uri)
						}
					}
					if len(kept) != len(uris) {
						p["uris"], _ = json.Marshal(kept)
						declaration["parameters"], _ = json.Marshal(p)
					}
				} else {
					registryMutex.RLock()
					clean := resourceProtectionCleaners[kind]
					if key == "workers" {
						clean = workerProtectionCleaners[kind]
					}
					registryMutex.RUnlock()
					if clean != nil {
						next, e := clean(params, match)
						if e != nil {
							return nil, e
						}
						if !json.Valid(next) {
							return nil, fmt.Errorf("invalid custom protection configuration")
						}
						declaration["parameters"] = next
					}
				}
			}
			fields[key], _ = json.Marshal(declarations)
		case "definition", "draft", "expert", "expert_draft", "pipeline", "active", "previous", "configuration_snapshot":
			next, e := purgeConfigurationJSON(value, match)
			if e != nil {
				return nil, e
			}
			fields[key] = next
		case "runs":
			var runs []json.RawMessage
			if e := json.Unmarshal(value, &runs); e != nil {
				return nil, e
			}
			for n, run := range runs {
				next, e := purgeConfigurationJSON(run, match)
				if e != nil {
					return nil, e
				}
				runs[n] = next
			}
			fields[key], _ = json.Marshal(runs)
		case "crawls":
			next, e := purgeRegistryJSON(value, match)
			if e != nil {
				return nil, e
			}
			fields[key] = next
		case "operations":
			var ops map[string]map[string]json.RawMessage
			if e := json.Unmarshal(value, &ops); e != nil {
				return nil, e
			}
			for _, op := range ops {
				for _, field := range []string{"error", "record"} {
					var text string
					if json.Unmarshal(op[field], &text) == nil && protection.References(text, match) {
						op[field] = json.RawMessage(`""`)
					}
				}
			}
			fields[key], _ = json.Marshal(ops)
		}
	}
	return json.Marshal(fields)
}
func purgeRegistryJSON(raw json.RawMessage, match func(string) bool) (json.RawMessage, error) {
	var records map[string]json.RawMessage
	if e := json.Unmarshal(raw, &records); e != nil {
		return nil, e
	}
	for key, value := range records {
		next, e := purgeConfigurationJSON(value, match)
		if e != nil {
			return nil, e
		}
		records[key] = next
	}
	return json.Marshal(records)
}
func purgeBackupJSON(raw json.RawMessage, match func(string) bool) (json.RawMessage, error) {
	var root map[string]json.RawMessage
	if e := json.Unmarshal(raw, &root); e != nil {
		return nil, e
	}
	if root["active"] != nil || root["crawls"] != nil {
		return purgeConfigurationJSON(raw, match)
	}
	for _, record := range root {
		var fields map[string]json.RawMessage
		if json.Unmarshal(record, &fields) != nil || fields["definition"] == nil {
			return nil, fmt.Errorf("unsupported configuration backup format")
		}
	}
	return purgeRegistryJSON(raw, match)
}
