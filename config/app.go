package config

import (
	"encoding/json"
	"time"
)

type Worker struct {
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Manager    string          `json:"manager"`
	Interval   time.Duration   `json:"interval"`
	Parameters json.RawMessage `json:"parameters"`
	Inbox      string          `json:"inbox"`
	Outbox     string          `json:"outbox"`
}

type Resource struct {
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Parameters json.RawMessage `json:"parameters"`
}

type RawApplication struct {
	Config json.RawMessage `json:"config"`
}

type Application struct {
	Config    Config     `json:"config"`
	Workers   []Worker   `json:"workers"`
	Resources []Resource `json:"resources"`
}
