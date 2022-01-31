package maps

type Map interface {
	Get([]byte) ([]byte, error)
	Set([]byte, []byte) error
	SetMany([][2][]byte) error
	IterKeys(func([]byte) error) error
}

type ErrKeyNotFound error
