package message

import "github.com/iakinsey/delver/types"

type FetcherRequest struct {
	RequestID types.UUID     `json:"request_id"`
	URI       string         `json:"uri"`
	Host      string         `json:"host"`
	Origin    string         `json:"origin"`
	Protocol  types.Protocol `json:"protocol"`
}
