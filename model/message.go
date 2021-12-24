package model

import "github.com/iakinsey/delver/types"

type Message struct {
	MessageType types.MessageType
	Message     []byte
}
