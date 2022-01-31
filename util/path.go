package util

import (
	"fmt"
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"sort"
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
