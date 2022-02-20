package maps

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"path"

	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
)

type multiHostMap struct {
	basePath string
	mapLock  util.KeyedMutex
}

type MultiHostMapParams struct {
	BasePath string `json:"base_path"`
}

func NewMultiHostMap(params MultiHostMapParams) Map {
	m := &multiHostMap{
		basePath: params.BasePath,
		mapLock:  *util.NewKeyedMutex(),
	}

	return m
}

func (s *multiHostMap) Get(key []byte) ([]byte, error) {
	return s.transaction(key, func(m Map) ([]byte, error) {
		return m.Get(key)
	})
}

func (s *multiHostMap) Set(key []byte, val []byte) (err error) {
	_, err = s.transaction(key, func(m Map) ([]byte, error) {
		return nil, m.Set(key, val)
	})

	return
}

func (s *multiHostMap) SetMany(pairs [][2][]byte) error {
	pairMap := make(map[string][][2][]byte)

	for _, pair := range pairs {
		u := pair[0]
		meta, err := url.Parse(string(u))

		if err != nil {
			return errors.Wrapf(err, "failed to parse url: %s", u)
		}

		pairMap[meta.Host] = append(pairMap[meta.Host], pair)
	}

	for key, pairs := range pairMap {
		u := []byte(fmt.Sprintf("http://%s/", key))

		_, err := s.transaction(u, func(m Map) ([]byte, error) {
			return nil, m.SetMany(pairs)
		})

		if err != nil {
			return errors.Wrap(err, "failed to write pairs")
		}
	}

	return nil
}

func (s *multiHostMap) Iter(fn func([]byte, []byte) error) error {
	return errors.New("multiDomain.Iter not implemented")
}

func (s *multiHostMap) Close() {
	// TODO find a way to close existing connections before exiting
}

func (s *multiHostMap) transaction(key []byte, fn func(m Map) ([]byte, error)) ([]byte, error) {
	u := string(key)
	meta, err := url.Parse(u)

	if err != nil {
		return nil, errors.Wrap(err, "transaction: failed to parse url")
	}
	mapKey := util.GetSLDAndTLD(meta.Host)
	fName := base64.URLEncoding.EncodeToString([]byte(mapKey))

	s.mapLock.Lock(mapKey)

	params := PersistentMapParams{
		Path: path.Join(s.basePath, fName),
	}
	mapper := NewPersistentMap(params)
	res, err := fn(mapper)

	mapper.Close()
	s.mapLock.Unlock(mapKey)

	return res, err
}
