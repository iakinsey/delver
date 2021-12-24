package model

import "github.com/iakinsey/delver/types"

type Request struct {
	RequestID types.UUID
	URI       types.URI
	Protocol  types.Protocol
}
