package types

import "github.com/google/uuid"

type Protocol string
type UUID string

func NewV4() UUID {
	return UUID(uuid.New().String())
}

const ProtocolHTTP Protocol = "HTTP"
