package types

type MessageType int32

const (
	NullMessage MessageType = iota
	FetcherRequestType
	FetcherResponseType
	CompositeAnalysisType
)
