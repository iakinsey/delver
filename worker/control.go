package worker

import "sync"

type Activity struct {
	LastError   string `json:"last_error,omitempty"`
	Active      int    `json:"active"`
	Paused      bool   `json:"paused"`
	Processed   uint64 `json:"processed"`
	Errors      uint64 `json:"errors"`
	LastSuccess int64  `json:"last_success,omitempty"`
}

type control struct {
	mu       sync.Mutex
	activity Activity
	changed  chan struct{}
}

func (c *control) pause(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.activity.Paused = value
	if c.changed != nil {
		close(c.changed)
		c.changed = nil
	}
}
func (c *control) snapshot() Activity { c.mu.Lock(); defer c.mu.Unlock(); return c.activity }
