package message

import "github.com/iakinsey/delver/types"

type FetcherRequest struct {
	RequestID types.UUID     `json:"request_id,omitempty"`
	URI       string         `json:"uri,omitempty"`
	Host      string         `json:"host,omitempty"`
	Origin    string         `json:"origin,omitempty"`
	Protocol  types.Protocol `json:"protocol,omitempty"`
}
