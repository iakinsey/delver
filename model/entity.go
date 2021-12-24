package model

import "github.com/iakinsey/delver/types"

type Entity struct {
	ID       types.UUID
	Response Response
	Features *CompositeAnalysis
}
