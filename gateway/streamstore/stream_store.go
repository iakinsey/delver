package streamstore

import (
	"io"

	"github.com/iakinsey/delver/types"
)

type StreamStore interface {
	Get(types.UUID) (io.Reader, error)
	Put(types.UUID, io.Reader) (string, error)
	Delete(types.UUID) error
}
