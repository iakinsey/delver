package rpc

import "encoding/json"

type SaveDashboardRequest struct {
	ID          string          `json:"id,omitempty"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Value       json.RawMessage `json:"value"`
}

type LoadDashboardRequest struct {
	ID string `json:"id"`
}

type DeleteDashboardRequest struct {
	ID string `json:"id"`
}
