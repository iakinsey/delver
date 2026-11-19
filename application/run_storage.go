package application

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/resource/maps"
)

// Guided-to-custom conversion changes the input format, not storage ownership.
// Copy only storage paths; custom parameters and their JSON integers stay raw.
func bindExistingStorage(next, previous config.Application) (config.Application, error) {
	bind := func(raw, old json.RawMessage) (json.RawMessage, error) {
		var value, paths map[string]json.RawMessage
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(old, &paths); err != nil {
			return nil, err
		}
		for _, key := range []string{"path", "base_path", "dlq_path", "crawl_id"} {
			if path, ok := paths[key]; ok {
				value[key] = path
			}
		}
		return json.Marshal(value)
	}
	next.Resources = append([]config.Resource(nil), next.Resources...)
	next.Workers = append([]config.Worker(nil), next.Workers...)
	for i, v := range next.Resources {
		for _, old := range previous.Resources {
			if old.Name == v.Name && old.Type == v.Type {
				raw, err := bind(v.Parameters, old.Parameters)
				if err != nil {
					return next, err
				}
				next.Resources[i].Parameters = raw
				break
			}
		}
	}
	for i, v := range next.Workers {
		for _, old := range previous.Workers {
			if old.Name == v.Name && old.Type == v.Type {
				raw, err := bind(v.Parameters, old.Parameters)
				if err != nil {
					return next, err
				}
				next.Workers[i].Parameters = raw
				break
			}
		}
	}
	return next, nil
}

// Storage identity is fixed for an existing run. Reject custom pipeline edits
// that would silently open an empty frontier or a different queue namespace.
func validateExistingStorage(previous, next config.Application) error {
	for _, old := range previous.Resources {
		var found *config.Resource
		for i := range next.Resources {
			if next.Resources[i].Name == old.Name {
				found = &next.Resources[i]
				break
			}
		}
		if found == nil {
			if old.Type == "frontier_store" || old.Type == "crawl_queue" {
				return fmt.Errorf("cannot remove stateful resource %s from an existing crawl", old.Name)
			}
			continue
		}
		var before, after map[string]json.RawMessage
		if err := json.Unmarshal(old.Parameters, &before); err != nil {
			continue // Custom resources may use scalar or array parameters.
		}
		storage := false
		for _, key := range []string{"path", "base_path", "dlq_path", "crawl_id"} {
			_, exists := before[key]
			storage = storage || exists
		}
		if !storage {
			continue
		}
		if err := json.Unmarshal(found.Parameters, &after); err != nil {
			return err
		}
		for _, key := range []string{"path", "base_path", "dlq_path", "crawl_id"} {
			if value, ok := before[key]; ok && (old.Type != found.Type || string(value) != string(after[key])) {
				return fmt.Errorf("cannot change storage identity %s.%s in an existing crawl", old.Name, key)
			}
		}
	}
	return nil
}

// Source checkpoints track one-shot publication, not visited URLs. Invalidate
// only changed sources after workers close. Durable admission deduplicates seeds.
func resetChangedSources(previous, next config.Application) (err error) {
	changed := []string{}
	for _, source := range next.Workers {
		if !source.Once {
			continue
		}
		unchanged := false
		for _, old := range previous.Workers {
			if old.Once && old.Name == source.Name && old.Type == source.Type && bytes.Equal(old.Parameters, source.Parameters) {
				unchanged = true
				break
			}
		}
		if !unchanged {
			changed = append(changed, source.Name)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	for _, resource := range next.Resources {
		if resource.Name != "source_progress" || resource.Type != "persistent_map" {
			continue
		}
		var params maps.PersistentMapParams
		if err := json.Unmarshal(resource.Parameters, &params); err != nil {
			return err
		}
		// This resource constructor reports open failures by panic.
		defer func() {
			if value := recover(); value != nil {
				err = fmt.Errorf("source checkpoint open: %v", value)
			}
		}()
		store := maps.NewPersistentMapWithConfig(params, next.Config.PersistentMap)
		defer store.Close()
		for _, key := range changed {
			if err := store.Set([]byte(key), nil); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("changed one-shot source requires source_progress storage")
}
