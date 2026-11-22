package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/application"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/worker"
	"sync"
	"sync/atomic"
)

var fixtureFailures sync.Map

func failureFlag(token string) *atomic.Bool {
	fresh := &atomic.Bool{}
	fresh.Store(true)
	actual, _ := fixtureFailures.LoadOrStore(token, fresh)
	return actual.(*atomic.Bool)
}

type fixtureFailureWorker struct{ flag *atomic.Bool }

func (s *fixtureFailureWorker) OnMessage(msg types.Message) (interface{}, error) {
	if s.flag.Load() {
		return nil, fmt.Errorf("fixture sink temporarily unavailable")
	}
	return nil, nil
}
func (s *fixtureFailureWorker) OnComplete() {}
func init() {
	application.RegisterWorkerWithRuntime("fixture_failure", func(_ context.Context, raw json.RawMessage, _ map[string]interface{}, _ application.RuntimeDependencies) (worker.Worker, error) {
		var p struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		if p.Token == "" {
			return nil, fmt.Errorf("fixture failure token required")
		}
		return &fixtureFailureWorker{flag: failureFlag(p.Token)}, nil
	}, application.ProtectionMetadataOnly)
}

var fixturePressure sync.Map

func pressureFlag(token string) *atomic.Bool {
	actual, _ := fixturePressure.LoadOrStore(token, &atomic.Bool{})
	return actual.(*atomic.Bool)
}

type fixturePressureResource struct{ flag *atomic.Bool }

func (r *fixturePressureResource) Health() string {
	if r.flag.Load() {
		return "injected storage pressure"
	}
	return ""
}
func init() {
	application.RegisterResourceWithRuntime("fixture_pressure", func(_ context.Context, raw json.RawMessage, _ application.RuntimeDependencies) (interface{}, error) {
		var p struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		if p.Token == "" {
			return nil, fmt.Errorf("pressure token required")
		}
		return &fixturePressureResource{pressureFlag(p.Token)}, nil
	}, application.ProtectionMetadataOnly)
}
