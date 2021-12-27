package streamstore

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/util"
)

type filesystemStreamStore struct {
	Path string
}

func NewFilesystemStreamStore(path string) (StreamStore, error) {
	return &filesystemStreamStore{Path: path}, nil
}

func (s *filesystemStreamStore) Get(uuid types.UUID) (io.Reader, error) {
	return os.Open(path.Join(s.Path, string(uuid)))
}

func (s *filesystemStreamStore) Put(uuid types.UUID, source io.Reader) (string, error) {
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

func (s *filesystemStreamStore) Delete(uuid types.UUID) error {
	return os.Remove(path.Join(s.Path, string(uuid)))
}
