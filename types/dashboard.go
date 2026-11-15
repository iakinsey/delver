package types

import "encoding/json"

type Dashboard struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Value       json.RawMessage `json:"value"`
	Description string          `json:"description,omitempty"`
}
