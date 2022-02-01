package maps

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"path"
	"time"

	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
)

type multiHostMap struct {
	basePath string
	maps     map[string]*hostMap
}

type hostMap struct {
	mapper  Map
	created int64
}

func NewMultiHostMap(basePath string) Map {
	m := &multiHostMap{
		basePath: basePath,
		maps:     make(map[string]*hostMap),
	}

	go m.clearMaps()

	return m
}

func (s *multiHostMap) Get(key []byte) ([]byte, error) {
	m, err := s.getOrSetHostMap(key)

	if err != nil {
		return nil, err
	}

	return m.mapper.Get(key)
}

func (s *multiHostMap) Set(key []byte, val []byte) error {
	m, err := s.getOrSetHostMap(key)

	if err != nil {
		return err
	}

	return m.mapper.Set(key, val)
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
		u := fmt.Sprintf("http://%s/", key)
		m, err := s.getOrSetHostMap([]byte(u))

		if err != nil {
			return err
		}

		if err := m.mapper.SetMany(pairs); err != nil {
			return err
		}
	}

	return nil
}

func (s *multiHostMap) Iter(fn func([]byte, []byte) error) error {
	return errors.New("multiDomain.Iter not implemented")
}

func (s *multiHostMap) Close() {
	for _, m := range s.maps {
		m.mapper.Close()
	}
}

func (s *multiHostMap) getOrSetHostMap(key []byte) (*hostMap, error) {
	u := string(key)
	meta, err := url.Parse(u)

	if err != nil {
		return nil, errors.Wrap(err, "getOrSetHostMap: failed to parse url")
	}

	val, ok := s.maps[meta.Host]

	if !ok {
		sld := []byte(util.GetSLD(meta.Host))
		fName := base64.StdEncoding.EncodeToString(sld)
		val = &hostMap{
			created: time.Now().Unix(),
			mapper:  NewPersistentMap(path.Join(s.basePath, fName)),
		}

		s.maps[meta.Host] = val
	}

	return val, nil
}

func (s *multiHostMap) clearMaps() {}
