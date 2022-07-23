package filter

import (
	"io"

	"github.com/iakinsey/delver/types/rpc"
)

type StreamFilter interface {
	Perform(entities interface{}) (interface{}, error)
}

type SearchFilter interface {
	Perform() (io.Reader, error)
}

func GetStreamFilter(params rpc.FilterParams) StreamFilter {
	return nil
}

func GetSearchFilter(params rpc.FilterParams) SearchFilter {
	return nil
}
