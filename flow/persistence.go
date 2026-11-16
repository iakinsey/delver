package flow

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

type persisted struct {
	Totals   Totals           `json:"totals"`
	Errors   map[string]int64 `json:"errors"`
	Statuses map[int]int64    `json:"statuses"`
	Counters map[string]int64 `json:"counters"`
}

func (c *Controller) restore() {
	if c.statePath == "" {
		return
	}
	f, err := os.Open(c.statePath)
	var data []byte
	if err == nil {
		data, err = io.ReadAll(io.LimitReader(f, 65537))
		f.Close()
		if len(data) > 65536 {
			err = io.ErrShortBuffer
		}
	}
	if os.IsNotExist(err) {
		return
	}
	var state persisted
	if err != nil || json.Unmarshal(data, &state) != nil {
		c.adjustment = "metrics history could not be restored"
		return
	}
	c.totals = state.Totals
	c.previous = state.Totals
	for key, value := range state.Errors {
		switch key {
		case "timeout", "connection", "proxy", "policy", "http_429", "http_5xx", "http_4xx", "body", "storage", "cancelled", "other":
			if value >= 0 {
				c.errors[key] = value
			}
		}
	}
	for status, value := range state.Statuses {
		if (status == 0 || status >= 100 && status <= 599) && value >= 0 {
			c.statuses[status] = value
		}
	}
	for key, value := range state.Counters {
		switch key {
		case "storage_conflicts", "storage_failures", "storage_operations":
			if value >= 0 {
				c.counters[key] = value
			}
		}
	}

}
func (c *Controller) save() {
	if c.statePath == "" {
		return
	}
	c.mu.Lock()
	data, err := json.Marshal(persisted{Totals: c.totals, Errors: c.errors, Statuses: c.statuses, Counters: c.counters})
	c.mu.Unlock()
	if err == nil {
		err = os.MkdirAll(filepath.Dir(c.statePath), 0700)
	}
	if err == nil {
		var f *os.File
		f, err = os.OpenFile(c.statePath+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err == nil {
			_, err = f.Write(data)
			if err == nil {
				err = f.Sync()
			}
			e := f.Close()
			if err == nil {
				err = e
			}
			if err == nil {
				err = os.Rename(c.statePath+".tmp", c.statePath)
			}
		}
	}
	if err != nil {
		c.mu.Lock()
		c.adjustment = "metrics history could not be saved"
		c.mu.Unlock()
	}
}
