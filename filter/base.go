package filter

import (
	"io"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/rpc"
)

type StreamFilter interface {
	Perform(entities []*types.Indexable) (interface{}, error)
}

type SearchFilter interface {
	Perform() (io.Reader, error)
}

func GetStreamFilter(params rpc.FilterParams) StreamFilter {
	// TODO START HERE NEXT
	// create all stream filters and transformers
	return nil
}

func GetSearchFilter(params rpc.FilterParams) SearchFilter {
	// TODO
	return nil
}
