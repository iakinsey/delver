package types

type MessageType int32

const (
	FetchRequest MessageType = iota
	FetchResponse
)
