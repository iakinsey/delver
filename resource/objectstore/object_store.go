package objectstore

import (
	"io"
	"os"

	"github.com/iakinsey/delver/types"
)

type ObjectStore interface {
	Get(types.UUID) (*os.File, error)
	Put(types.UUID, io.Reader) (string, error)
	Delete(types.UUID) error
}
