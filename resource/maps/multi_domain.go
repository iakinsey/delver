package maps

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"

	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
)

type multiHostMap struct {
	basePath string
}

type MultiHostMapParams struct {
	BasePath string `json:"base_path"`
}

func NewMultiHostMap(params MultiHostMapParams) Map {
	if err := util.GetOrCreateDir(params.BasePath); err != nil {
		panic(err)
	}
	m := &multiHostMap{
		basePath: params.BasePath,
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

		group := util.GetSLDAndTLD(meta.Host)
		pairMap[group] = append(pairMap[group], pair)
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
	entries, err := os.ReadDir(s.basePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		_, err := base64.URLEncoding.DecodeString(entry.Name())
		if err != nil {
			return err
		}
		hostPath := path.Join(s.basePath, entry.Name())
		err = func() error {
			lockHostPath(hostPath)
			defer unlockHostPath(hostPath)
			m := NewPersistentMap(PersistentMapParams{Path: hostPath})
			defer m.Close()
			return m.Iter(fn)
		}()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *multiHostMap) Close() {
}

func (s *multiHostMap) transaction(key []byte, fn func(m Map) ([]byte, error)) ([]byte, error) {
	u := string(key)
	meta, err := url.Parse(u)

	if err != nil {
		return nil, errors.Wrap(err, "transaction: failed to parse url")
	}
	mapKey := util.GetSLDAndTLD(meta.Host)
	fName := base64.URLEncoding.EncodeToString([]byte(mapKey))

	hostPath := path.Join(s.basePath, fName)
	lockHostPath(hostPath)
	defer unlockHostPath(hostPath)

	params := PersistentMapParams{
		Path: path.Join(s.basePath, fName),
	}
	mapper := NewPersistentMap(params)
	defer mapper.Close()
	res, err := fn(mapper)

	return res, err
}

var domainLocks = util.NewKeyedMutex()

func lockHostPath(p string) {
	absolute, err := filepath.Abs(p)
	if err != nil {
		absolute = p
	}
	domainLocks.Lock(absolute)
}
func unlockHostPath(p string) {
	absolute, err := filepath.Abs(p)
	if err != nil {
		absolute = p
	}
	domainLocks.Unlock(absolute)
}
func ConsumeHost(p string, consume func(Map) error) error {
	lockHostPath(p)
	defer unlockHostPath(p)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	mapper := NewPersistentMap(PersistentMapParams{Path: p})
	err := func() error { defer mapper.Close(); return consume(mapper) }()
	if err != nil {
		return err
	}
	return os.RemoveAll(p)
}
