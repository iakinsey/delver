package message

import "github.com/iakinsey/delver/types"

type FetcherRequest struct {
	RequestID types.UUID     `json:"request_id"`
	URI       string         `json:"uri"`
	Protocol  types.Protocol `json:"protocol"`
}
