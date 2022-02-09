package util

import (
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"sort"

	log "github.com/sirupsen/logrus"
)

const folderQueueDefaultPerms = 0755

func GetOrCreateDir(path string) (err error) {
	info, err := os.Stat(path)

	if os.IsNotExist(err) {
		return os.Mkdir(path, folderQueueDefaultPerms)
	}

	if !info.IsDir() {
		return fmt.Errorf("path exists and is not a directory: %s", path)
	}

	return
}

func CreateFileOrFail(path string) (*os.File, error) {
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		return nil, os.ErrExist
	}

	return os.Create(path)
}

func CreateEmptyFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0644)
}

func GetOrCreateFile(path string) (*os.File, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.Create(path)
	} else if err != nil {
		return nil, err
	}

	return os.Open(path)
}

func ReadDirAlphabetized(path string) ([]fs.FileInfo, error) {
	files, err := ioutil.ReadDir(path)

	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})

	return files, err
}

func MakeTempFile(name string) *os.File {
	f, err := os.CreateTemp("", name)

	if err != nil {
		log.Fatal(err)
	}

	return f
}

func MakeTempFolder(name string) string {
	path, err := os.MkdirTemp("", name)

	if err != nil {
		log.Fatal(err)
	}

	return path
}

func NewTempPath(name string) string {
	return fmt.Sprintf("%s/%s%s", os.TempDir(), name, RandomString(8))
}

func PathExists(path string) (bool, error) {
	if path == "" {
		return false, nil
	} else if _, err := os.Stat(path); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}

	return true, nil
}
