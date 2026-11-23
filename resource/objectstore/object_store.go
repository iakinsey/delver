package objectstore

import (
	"context"
	"io"
	"os"

	"github.com/iakinsey/delver/types"
)

type ObjectStore interface {
	Get(types.UUID) (*os.File, error)
	Put(types.UUID, io.Reader) (string, error)
	Delete(types.UUID) error
}

// FileObjectStore optionally publishes an inspected, seekable staging file.
// Implementations still calculate their own digest and publish atomically.
// The byte count reports data actually copied with splice, not an estimate.
type FileObjectStore interface {
	ObjectStore
	PutFile(context.Context, types.UUID, *os.File) (string, int64, error)
}
