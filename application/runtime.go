package application

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/worker"
)

type runtimeKey struct{}
type runtimeControl struct {
	log           *crawlLog
	initialPaused bool
	resourcesDone chan struct{}
	mu            sync.Mutex
	prepared      *preparedApplication
}
type RuntimeSnapshot struct {
	Frontier    map[string]int64            `json:"frontier"`
	QueueStates map[string]map[string]int64 `json:"queue_states"`
	Health      map[string]string           `json:"health"`
	Workers     map[string]worker.Activity  `json:"workers"`
	Queues      map[string]int64            `json:"queues"`
}

func (c *runtimeControl) snapshot() RuntimeSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := RuntimeSnapshot{Frontier: map[string]int64{}, QueueStates: map[string]map[string]int64{}, Health: map[string]string{}, Workers: map[string]worker.Activity{}, Queues: map[string]int64{}}
	if c.prepared == nil {
		return result
	}
	for name, manager := range c.prepared.workers {
		if source, ok := manager.(interface{ Health() string }); ok {
			if health := source.Health(); health != "" {
				result.Health[name] = health
			}
		}
		if m, ok := manager.(interface{ Activity() worker.Activity }); ok {
			result.Workers[name] = m.Activity()
		}
	}
	for name, resource := range c.prepared.resources {
		if source, ok := resource.(interface {
			Counts() (map[string]int64, error)
		}); ok {
			counts, err := source.Counts()
			if err != nil {
				result.Health[name] = err.Error()
			} else {
				result.QueueStates[name] = counts
				if _, ok := resource.(frontier.FrontierStore); ok {
					for key, n := range counts {
						result.Frontier[key] += n
					}
				}
			}
		}
		if health, ok := resource.(interface{ Health() string }); ok {
			if err := health.Health(); err != "" {
				result.Health[name] = err
			}
		}
		if q, ok := resource.(queue.Queue); ok {
			result.Queues[name] = q.Len()
		}
	}
	return result
}
func (c *runtimeControl) pause(ctx context.Context, paused bool) error {
	c.mu.Lock()
	if c.prepared == nil {
		c.mu.Unlock()
		return fmt.Errorf("runtime is not ready")
	}
	prepared := c.prepared
	for _, definition := range prepared.app.Workers {
		if !paused || definition.Manager == "job" {
			prepared.workers[definition.Name].(interface{ Pause(bool) }).Pause(paused)
		}
	}
	c.mu.Unlock()
	if !paused {
		return nil
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	stable := 0
	for {
		if c.resourcesDone != nil {
			select {
			case <-c.resourcesDone:
				return fmt.Errorf("runtime stopped while draining")
			default:
			}
		}
		snapshot := c.snapshot()
		busy := false
		for _, activity := range snapshot.Workers {
			busy = busy || activity.Active > 0
		}
		for _, definition := range prepared.app.Workers {
			if definition.Manager != "job" {
				if count, ok := snapshot.Queues[definition.Inbox]; ok {
					busy = busy || count != 0
				}
			}
		}
		if busy {
			stable = 0
		} else {
			stable++
		}
		if stable >= 3 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, manager := range prepared.workers {
		manager.(interface{ Pause(bool) }).Pause(true)
	}
	return nil
}
