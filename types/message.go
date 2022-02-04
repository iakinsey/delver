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

func NewMessage(in interface{}, t MessageType) (Message, error) {
	jsonMessage, err := json.Marshal(in)

	if err != nil {
		return Message{}, err
	}

	return Message{
		ID:          "0-0-0-TestName",
		MessageType: t,
		Message:     json.RawMessage(jsonMessage),
	}, nil
}
