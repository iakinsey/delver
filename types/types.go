package types

import "github.com/google/uuid"

type HTTPCode string
type MD5 string
type URI string
type Protocol string
type UUID string

func NewV4() UUID {
	return UUID(uuid.New().String())
}
