package types

import "encoding/json"

type Dashboard struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	UserID      string          `json:"user_uid"`
	Value       json.RawMessage `json:"value"`
	Description string          `json:"description,omitempty"`
}
