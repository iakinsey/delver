package objectstore

import (
	"crypto/md5"
	"fmt"
	"github.com/google/uuid"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
)

type filesystemObjectStore struct {
	Path string
}

type FilesystemObjectStoreParams struct {
	Path string `json:"path"`
}

func NewFilesystemObjectStore(params FilesystemObjectStoreParams) ObjectStore {
	if err := util.GetOrCreateDir(params.Path); err != nil {
		panic(err)
	}

	return &filesystemObjectStore{Path: params.Path}
}

func (s *filesystemObjectStore) Get(uuid types.UUID) (*os.File, error) {
	if uuid == "" || filepath.Base(string(uuid)) != string(uuid) {
		return nil, errors.New("empty object key")
	}
	return os.Open(path.Join(s.Path, string(uuid)))
}

func (s *filesystemObjectStore) Put(uuid types.UUID, source io.Reader) (string, error) {
	if uuid == "" || filepath.Base(string(uuid)) != string(uuid) {
		return "", errors.New("invalid object key")
	}
	file, err := os.CreateTemp(s.Path, ".object-*")
	if err != nil {
		return "", err
	}
	defer file.Close()
	defer os.Remove(file.Name())
	hash := md5.New()
	if _, err = io.Copy(io.MultiWriter(hash, file), source); err != nil {
		return "", err
	}
	if err = file.Sync(); err != nil {
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(file.Name(), path.Join(s.Path, string(uuid))); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func (s *filesystemObjectStore) Delete(uuid types.UUID) error {
	if uuid == "" || filepath.Base(string(uuid)) != string(uuid) {
		return errors.New("empty key provided for delete request")
	}

	return os.Remove(path.Join(s.Path, string(uuid)))
}

func (s *filesystemObjectStore) Reconcile(retained map[types.UUID]bool) error {
	entries, err := os.ReadDir(s.Path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		_, parseErr := uuid.Parse(name)
		if strings.HasPrefix(name, ".object-") || (parseErr == nil && !retained[types.UUID(name)]) {
			if err := os.Remove(filepath.Join(s.Path, name)); err != nil {
				return err
			}
		}
	}
	return nil
}
