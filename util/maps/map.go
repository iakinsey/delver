package maps

import "io"

type Mapper interface {
	AddString(string) error
	AddBytes([]byte) error
	ContainsString(string) bool
	ContainsBytes([]byte) bool
	Size() uint64
	Save(io.Writer) (int64, error)
}
