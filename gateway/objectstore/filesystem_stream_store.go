package objectstore

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/util"
)

type filesystemObjectStore struct {
	Path string
}

func NewFilesystemObjectStore(path string) (ObjectStore, error) {
	return &filesystemObjectStore{Path: path}, nil
}

func (s *filesystemObjectStore) Get(uuid types.UUID) (*os.File, error) {
	return os.Open(path.Join(s.Path, string(uuid)))
}

func (s *filesystemObjectStore) Put(uuid types.UUID, source io.Reader) (string, error) {
	md5sum := md5.New()
	path := path.Join(s.Path, string(uuid))
	file, err := util.CreateFileOrFail(path)

	if err != nil {
		return "", err
	}

	writer := io.MultiWriter(md5sum, file)

	defer file.Close()

	_, err = io.Copy(writer, source)

	return fmt.Sprintf("%x", md5sum.Sum(nil)), err
}

func (s *filesystemObjectStore) Delete(uuid types.UUID) error {
	return os.Remove(path.Join(s.Path, string(uuid)))
}
