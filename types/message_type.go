package types

type MessageType int32

const (
	NullMessage MessageType = iota
	FetchRequest
	FetchResponse
)
