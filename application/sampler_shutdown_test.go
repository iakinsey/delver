package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/frontier"
	"github.com/stretchr/testify/require"
	"sync/atomic"
	"testing"
	"time"
)

type blockedSamplerStore struct {
	*frontier.Store
	calls                    atomic.Int32
	entered, release, closed chan struct{}
}

func (s *blockedSamplerStore) Counts() (map[string]int64, error) {
	if s.calls.Add(1) == 2 {
		close(s.entered)
		<-s.release
	}
	select {
	case <-s.closed:
		return nil, fmt.Errorf("counts after close")
	default:
		return s.Store.Counts()
	}
}
func (s *blockedSamplerStore) Close() error { close(s.closed); return s.Store.Close() }

func TestShutdownJoinsPressureSamplerBeforeClosingStorage(t *testing.T) {
	d := DefaultCrawlDefinition()
	d.Seeds = []string{"http://example.test/"}
	d.Fetch.SampleInterval = flow.Duration(10 * time.Millisecond)
	app, err := CompileCrawl(t.TempDir(), "sampler", d)
	require.NoError(t, err)
	app.Config.Workers.Enabled = false
	var store *blockedSamplerStore
	RegisterResourceWithRuntime("fixture_blocked_sampler", func(_ context.Context, raw json.RawMessage, _ RuntimeDependencies) (interface{}, error) {
		var p frontier.StoreParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		s, err := frontier.OpenStore(p)
		if err != nil {
			return nil, err
		}
		store = &blockedSamplerStore{Store: s, entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
		return store, nil
	})
	defer RegisterResourceWithRuntime("fixture_blocked_sampler", nil)
	// Pass the instance through a channel so test reads never race construction.
	ready := make(chan *blockedSamplerStore, 1)
	original := resourceFactories["fixture_blocked_sampler"]
	RegisterResourceWithRuntime("fixture_blocked_sampler", func(ctx context.Context, raw json.RawMessage, deps RuntimeDependencies) (interface{}, error) {
		v, e := original(ctx, raw, deps)
		if e == nil {
			ready <- v.(*blockedSamplerStore)
		}
		return v, e
	})
	for i, r := range app.Resources {
		if r.Type == "frontier_store" {
			app.Resources[i].Type = "fixture_blocked_sampler"
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, app) }()
	select {
	case store = <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("resource was not constructed")
	}
	select {
	case <-store.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("sampler did not enter counts")
	}
	cancel()
	premature := false
	select {
	case <-store.closed:
		premature = true
	case <-time.After(100 * time.Millisecond):
	}
	close(store.release)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	require.False(t, premature, "storage closed while the pressure sampler was reading it")
}
