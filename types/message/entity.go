package message

import (
	"github.com/iakinsey/delver/types"
)

type Entity struct {
	ID       types.UUID         `json:"id,omitempty"`
	Response FetcherResponse    `json:"id,omitempty"`
	Features *CompositeAnalysis `json:"id,omitempty"`
}
