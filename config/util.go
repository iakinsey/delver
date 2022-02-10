package config

import (
	"flag"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

func DataFilePath(dataPath string, name string) string {
	_, b, _, ok := runtime.Caller(0)

	if !ok {
		log.Fatalf("failed to get base path for data file")
	}

	basepath := ""

	if IsTest() {
		basepath = path.Dir(path.Dir(b))
	} else if p, err := os.Executable(); err != nil {
		log.Fatalf(err.Error())
	} else {
		basepath = path.Dir(p)
	}

	return filepath.Join(basepath, dataPath, name)
}

func IsTest() bool {
	if flag.Lookup("test.v") != nil {
		return true
	} else if strings.HasSuffix(os.Args[0], ".test") {
		return true
	} else if len(os.Args) > 2 && strings.HasSuffix(os.Args[1], "test.run") {
		return true
	}

	return false
}
