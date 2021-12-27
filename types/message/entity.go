package message

import (
	"github.com/iakinsey/delver/types"
)

type Entity struct {
	ID       types.UUID
	Response FetcherResponse
	Features *types.CompositeAnalysis
}
