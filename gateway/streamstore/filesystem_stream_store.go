package streamstore

import (
	"io"
	"os"
	"path"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/util"
)

type FilesystemStreamStoreArgs struct {
	Path string
}

type filesystemStreamStore struct {
	FilesystemStreamStoreArgs
}

func NewFilesystemStreamStore(arg FilesystemStreamStoreArgs) (StreamStore, error) {
	streamStore := filesystemStreamStore{arg}

	return &streamStore, nil
}

func (s *filesystemStreamStore) Get(uuid types.UUID) (io.Reader, error) {
	return os.Open(path.Join(s.Path, string(uuid)))
}

func (s *filesystemStreamStore) Put(uuid types.UUID, source io.Reader) error {
	path := path.Join(s.Path, string(uuid))

	file, err := util.CreateFileOrFail(path)

	if err != nil {
		return err
	}

	defer file.Close()

	_, err = io.Copy(file, source)

	return err
}

func (s *filesystemStreamStore) Delete(uuid types.UUID) error {
	return os.Remove(path.Join(s.Path, string(uuid)))
}
