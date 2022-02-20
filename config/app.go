package config

import "encoding/json"

type Worker struct {
	Name       string          `json:"string"`
	Type       string          `json:"type"`
	Parameters json.RawMessage `json:"parameters"`
	Inbox      string          `json:"inbox"`
	Outbox     string          `json:"outbox"`
}

type Resource struct {
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Parameters json.RawMessage `json:"parameters"`
}

type Application struct {
	Config    Config     `json:"config"`
	Workers   []Worker   `json:"workers"`
	Resources []Resource `json:"resources"`
}
