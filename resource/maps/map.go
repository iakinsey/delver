package maps

import "errors"

type Map interface {
	Get([]byte) ([]byte, error)
	Set([]byte, []byte) error
	SetMany([][2][]byte) error
	Iter(func(k []byte, v []byte) error) error
	Close()
}

var ErrKeyNotFound = errors.New("no such key")
