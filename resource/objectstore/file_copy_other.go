//go:build !linux

package objectstore

import (
	"context"
	"os"
)

func copyFileBody(ctx context.Context, destination, source *os.File) (int64, error) {
	return 0, copyFileBuffered(ctx, destination, source)
}
