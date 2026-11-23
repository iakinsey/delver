package objectstore

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/iakinsey/delver/types"
)

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func copyFileBuffered(ctx context.Context, destination, source *os.File) error {
	buffer := make([]byte, 32<<10)
	defer clear(buffer)
	_, err := io.CopyBuffer(struct{ io.Writer }{destination}, contextReader{ctx, source}, buffer)
	return err
}

func (s *filesystemObjectStore) PutFile(ctx context.Context, key types.UUID, source *os.File) (string, int64, error) {
	if key == "" || filepath.Base(string(key)) != string(key) {
		return "", 0, fmt.Errorf("invalid object key")
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	info, err := source.Stat()
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("regular staging file required")
	}
	hash := md5.New()
	buffer := make([]byte, 32<<10)
	defer clear(buffer)
	if _, err = io.CopyBuffer(hash, contextReader{ctx, io.NewSectionReader(source, 0, info.Size())}, buffer); err != nil {
		return "", 0, err
	}
	if _, err = source.Seek(0, io.SeekStart); err != nil {
		return "", 0, err
	}
	file, err := os.CreateTemp(s.Path, ".object-*")
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	defer os.Remove(file.Name())
	spliced, err := copyFileBody(ctx, file, source)
	if err != nil {
		return "", spliced, err
	}
	written, err := file.Stat()
	if err != nil {
		return "", spliced, err
	}
	if written.Size() != info.Size() {
		return "", spliced, io.ErrUnexpectedEOF
	}
	if err = ctx.Err(); err != nil {
		return "", spliced, err
	}
	if err = file.Sync(); err != nil {
		return "", spliced, err
	}
	if err = file.Close(); err != nil {
		return "", spliced, err
	}
	if err = ctx.Err(); err != nil {
		return "", spliced, err
	}
	if err = os.Rename(file.Name(), filepath.Join(s.Path, string(key))); err != nil {
		return "", spliced, err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), spliced, nil
}
