package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/protection"
)

const ProtectionAware = "workspace_protection"
const ProtectionMetadataOnly = "protection_metadata_only"

var workerProtection = map[string]string{}
var resourceProtection = map[string]string{}

// ProtectionSupport extends constructor registration with a pure configuration
// cleaner. Runtime participants must implement the three enforcement methods.
type ProtectionCapability interface{}
type ProtectionConfigurationCleaner func(json.RawMessage, func(string) bool) (json.RawMessage, error)
type ProtectionSupport struct {
	Configuration ProtectionConfigurationCleaner
}
type ProtectionParticipant interface {
	SetProtection(*protection.Policy)
	PurgeMatching(context.Context, func(string) bool) error
	VerifyNoMatches(context.Context, func(string) bool) error
}

var workerProtectionCleaners = map[string]ProtectionConfigurationCleaner{}
var resourceProtectionCleaners = map[string]ProtectionConfigurationCleaner{}

func declaredProtection(values []ProtectionCapability) (string, ProtectionConfigurationCleaner) {
	for _, v := range values {
		switch c := v.(type) {
		case string:
			if c == ProtectionMetadataOnly {
				return c, nil
			}
		case ProtectionSupport:
			if c.Configuration != nil {
				return ProtectionAware, c.Configuration
			}
		}
	}
	return "", nil
}
func checkProtectionParticipant(value interface{}, policy *protection.Policy, kind, name string) error {
	if policy == nil {
		return nil
	}
	registryMutex.RLock()
	capability := resourceProtection[name]
	if kind == "worker" {
		capability = workerProtection[name]
	}
	registryMutex.RUnlock()
	if capability == ProtectionAware {
		if _, ok := value.(ProtectionParticipant); !ok {
			return fmt.Errorf("%s %s lacks runtime enforcement, purge or verification methods", kind, name)
		}
	}
	return nil
}

// Capability declarations extend the existing constructor registry. They do not
// create a second plugin system, nor infer safety from a constructor's name.
func validateProtection(a config.Application) error {
	if a.Config.HTTPClient.Protection == nil {
		return nil
	}
	for _, w := range a.Workers {
		switch w.Type {
		case "http_fetcher", "fixed_seed_publisher", "rss_feed_publisher", "composite_extractor", "frontier_admission", "frontier_accumulator", "frontier_publisher", "frontier_cleanup", "jsonl_sink", "transformer":
		default:
			if workerProtection[w.Type] == "" {
				return fmt.Errorf("worker %s does not declare workspace protection and purge support; migrate or update its registration", w.Type)
			}
		}
	}
	for _, r := range a.Resources {
		switch r.Type {
		case "frontier_store", "crawl_queue", "file_queue", "filesystem_object_store":
		case "persistent_map":
			if r.Name != "source_progress" && resourceProtection[r.Type] == "" {
				return fmt.Errorf("resource %s requires workspace protection and purge support", r.Name)
			}
		default:
			if resourceProtection[r.Type] == "" {
				return fmt.Errorf("resource %s does not declare workspace protection and purge support; migrate or update its registration", r.Type)
			}
		}
	}
	return nil
}
