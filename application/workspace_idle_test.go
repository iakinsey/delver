package application

import (
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/worker"
	"testing"
)

func TestCompletionIgnoresOnlyPeriodicFrontierMaintenance(t *testing.T) {
	for _, tc := range []struct {
		kind, manager string
		idle          bool
	}{
		{"frontier_publisher", "job", true},
		{"frontier_cleanup", "job", true},
		{"fixed_seed_publisher", "job", false},
		{"http_fetcher", "worker", false},
		{"composite_extractor", "worker", false},
		{"frontier_accumulator", "worker", false},
		{"jsonl_sink", "worker", false},
		{"frontier_publisher", "worker", false},
	} {
		t.Run(tc.kind+"/"+tc.manager, func(t *testing.T) {
			activities := map[string]worker.Activity{"busy": {Active: 1}}
			definitions := []config.Worker{{Name: "busy", Type: tc.kind, Manager: tc.manager}}
			if got := crawlWorkersIdle(activities, definitions); got != tc.idle {
				t.Fatalf("idle=%v, want %v", got, tc.idle)
			}
			activities["busy"] = worker.Activity{}
			if !crawlWorkersIdle(activities, definitions) {
				t.Fatal("inactive worker blocked completion")
			}
			activities["unknown"] = worker.Activity{Active: 1}
			if crawlWorkersIdle(activities, definitions) {
				t.Fatal("unknown active worker ignored")
			}
		})
	}
}
