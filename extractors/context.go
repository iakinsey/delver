package extractors

import (
	"context"
	"io"
)

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

type contextRuneReader struct {
	ctx    context.Context
	reader io.RuneReader
}

func (r contextRuneReader) ReadRune() (rune, int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, 0, err
	}
	return r.reader.ReadRune()
}
