package util

import (
	"fmt"
	"io/fs"
	"io/ioutil"
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
		return fmt.Errorf("Path exists and is not a directory: %s", path)
	}

	return
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
