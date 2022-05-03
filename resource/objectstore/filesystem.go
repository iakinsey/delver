package objectstore

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type filesystemObjectStore struct {
	Path string
}

type FilesystemObjectStoreParams struct {
	Path string `json:"path"`
}

func NewFilesystemObjectStore(params FilesystemObjectStoreParams) ObjectStore {
	if err := util.GetOrCreateDir(params.Path); err != nil {
		log.Fatalf("failed to set up filesystem object store directory, %s", err)
	}

	return &filesystemObjectStore{Path: params.Path}
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

	return fmt.Sprintf("%x", md5sum.Sum(nil)), errors.Wrap(err, "failed to put object")
}

func (s *filesystemObjectStore) Delete(uuid types.UUID) error {
	if uuid == "" {
		return errors.New("empty key provided for delete request")
	}

	return os.Remove(path.Join(s.Path, string(uuid)))
}
