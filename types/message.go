package types

import (
	"encoding/json"
)

type Message struct {
	ID          string          `json:"id"`
	MessageType MessageType     `json:"message_type"`
	Message     json.RawMessage `json:"message"`
}

type MultiMessage struct {
	Values []interface{}
}
