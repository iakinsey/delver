package model

import (
	"encoding/json"

	"github.com/iakinsey/delver/types"
)

type Message struct {
	ID          string            `json:"id"`
	MessageType types.MessageType `json:"message_type"`
	Message     json.RawMessage   `json:"message"`
}
