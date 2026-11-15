package application

import (
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/worker"
	"github.com/iakinsey/delver/worker/frontierworker"
	"regexp"
)

func init() {
	RegisterWorker("frontier_admission", func(raw json.RawMessage, resources map[string]interface{}) (worker.Worker, error) {
		var p struct {
			Store               string   `json:"frontier_store"`
			AllowedHostSuffixes []string `json:"allowed_host_suffixes,omitempty"`
		}
		if err := decodeParameters(raw, &p); err != nil {
			return nil, err
		}
		store, ok := resources[p.Store].(frontier.FrontierStore)
		if !ok {
			return nil, fmt.Errorf("frontier_store required")
		}
		suffixes, err := util.NormalizeHostSuffixes(p.AllowedHostSuffixes)
		if err != nil {
			return nil, err
		}
		return &frontierworker.Admission{Store: store, AllowedHostSuffixes: suffixes}, nil
	})
	RegisterResource("frontier_store", func(raw json.RawMessage) (interface{}, error) {
		p := frontier.StoreParams{Retention: frontier.DefaultRetention()}
		if err := decodeParameters(raw, &p); err != nil {
			return nil, err
		}
		return frontier.OpenStore(p)
	})
	for name, kind := range map[string]string{"frontier_accumulator": "accumulator", "frontier_publisher": "publisher", "frontier_cleanup": "cleanup"} {
		kind := kind
		RegisterWorker(name, func(raw json.RawMessage, resources map[string]interface{}) (worker.Worker, error) {
			var refs struct {
				Store  string `json:"frontier_store"`
				Output string `json:"output_queue"`
			}
			if err := json.Unmarshal(raw, &refs); err != nil {
				return nil, err
			}
			var p frontierworker.Params
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
			p.Store, _ = resources[refs.Store].(frontier.FrontierStore)
			if refs.Output != "" {
				p.Output, _ = resources[refs.Output].(queue.Queue)
			}
			return frontierworker.New(p, kind)
		})
	}
}

func paramsFrontier(p frontier.StoreParams) error {
	if p.Path == "" || p.MaxPending < 0 || p.MaxPendingPerOrigin < 0 || p.MaxURLs < 0 || p.MaxURLsPerOrigin < 0 {
		return fmt.Errorf("invalid frontier storage limits")
	}
	if p.CacheBudgetMB != 0 && (p.CacheBudgetMB < 32 || p.CacheBudgetMB > 4096) {
		return fmt.Errorf("cache_budget_mb must be between 32 and 4096")
	}
	for pattern, limit := range p.PatternBudgets {
		if _, err := regexp.Compile(pattern); err != nil {
			return err
		}
		if limit < 1 {
			return fmt.Errorf("pattern budget must be positive")
		}
	}
	return p.Retention.Validate()
}
func validateFrontierWorker(w config.Worker, resources map[string]config.Resource) error {
	switch w.Type {
	case "frontier_accumulator", "frontier_publisher", "frontier_cleanup", "frontier_admission":
	default:
		return nil
	}
	var refs struct {
		Store  string `json:"frontier_store"`
		Output string `json:"output_queue"`
	}
	if err := json.Unmarshal(w.Parameters, &refs); err != nil {
		return err
	}
	resource, ok := resources[refs.Store]
	if !ok || (resource.Type != "frontier_store" && resourceFactories[resource.Type] == nil) {
		return fmt.Errorf("worker %s requires frontier_store", w.Name)
	}
	if w.Type == "frontier_publisher" || w.Type == "frontier_cleanup" {
		if w.Count > 1 {
			return fmt.Errorf("frontier job %s requires at most one worker", w.Name)
		}
		if w.Manager != "job" {
			return fmt.Errorf("worker %s must use job manager", w.Name)
		}
	}
	if w.Type == "frontier_publisher" && resources[refs.Output].Type != "crawl_queue" {
		return fmt.Errorf("frontier publisher requires durable crawl_queue")
	}
	if w.Type == "frontier_admission" {
		var p struct {
			Store               string   `json:"frontier_store"`
			AllowedHostSuffixes []string `json:"allowed_host_suffixes,omitempty"`
		}
		if err := decodeParameters(w.Parameters, &p); err != nil {
			return err
		}
		_, err := util.NormalizeHostSuffixes(p.AllowedHostSuffixes)
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(w.Parameters, &fields); err != nil {
		return err
	}
	delete(fields, "frontier_store")
	delete(fields, "output_queue")
	raw, _ := json.Marshal(fields)
	var p frontierworker.Params
	if err := decodeParameters(raw, &p); err != nil {
		return err
	}
	p.Store = &frontier.Store{}
	kind := "accumulator"
	if w.Type == "frontier_cleanup" {
		kind = "cleanup"
	}
	_, err := frontierworker.New(p, kind)
	return err
}
