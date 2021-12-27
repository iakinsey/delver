package message

import "github.com/iakinsey/delver/types"

type FetcherRequest struct {
	RequestID types.UUID     `json:"request_id"`
	URI       types.URI      `json:"uri"`
	Protocol  types.Protocol `json:"protocol"`
}
