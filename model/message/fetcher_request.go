package message

import "github.com/iakinsey/delver/types"

type FetcherRequest struct {
	RequestID types.UUID
	URI       types.URI
	Protocol  types.Protocol
}
