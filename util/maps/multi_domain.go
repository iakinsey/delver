package maps

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"path"
	"sync"
	"time"

	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
)

var pruneInterval = 5 * time.Minute
var mapKeepAliveTime = 5 * time.Minute

type multiHostMap struct {
	basePath  string
	maps      map[string]*hostMap
	terminate chan bool
	mapLock   sync.RWMutex
}

type hostMap struct {
	mapper  Map
	expires time.Time
}

func NewMultiHostMap(basePath string) Map {
	m := &multiHostMap{
		basePath:  basePath,
		maps:      make(map[string]*hostMap),
		terminate: make(chan bool),
		mapLock:   sync.RWMutex{},
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
	s.terminate <- true

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

	mapKey := util.GetSLDAndTLD(meta.Host)

	s.mapLock.RLock()

	val, ok := s.maps[mapKey]

	s.mapLock.RUnlock()

	if !ok {

		fName := base64.URLEncoding.EncodeToString([]byte(mapKey))
		val = &hostMap{
			expires: time.Now().Add(mapKeepAliveTime),
			mapper:  NewPersistentMap(path.Join(s.basePath, fName)),
		}

		s.mapLock.Lock()

		s.maps[mapKey] = val

		s.mapLock.Unlock()
	}

	return val, nil
}

func (s *multiHostMap) clearMaps() {
	for {
		select {
		case <-time.After(pruneInterval):
			var toDelete []string
			now := time.Now()

			s.mapLock.RLock()

			for key, val := range s.maps {
				if val.expires.After(now) {
					toDelete = append(toDelete, key)
					val.mapper.Close()
				}
			}

			s.mapLock.RUnlock()

			s.mapLock.Lock()
			for _, key := range toDelete {
				delete(s.maps, key)
			}
			s.mapLock.Unlock()

		case <-s.terminate:
			return
		}
	}
}
