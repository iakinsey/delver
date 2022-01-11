package util

import (
	"bufio"
	"flag"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/iakinsey/delver/config"
)

func DedupeStrSlice(slice []string) (deduped []string) {
	if len(slice) == 0 {
		return deduped
	}

	keys := make(map[string]bool)

	for _, entity := range slice {
		if _, value := keys[entity]; !value {
			keys[entity] = true
			deduped = append(deduped, entity)
		}
	}
	return deduped
}

func ReadLines(file *os.File) (lines []string, err error) {
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func ResolveUrls(base *url.URL, urls []string) (result []string) {
	for _, rawUrl := range urls {
		u, err := url.Parse(rawUrl)

		if err != nil {
			continue
		}

		result = append(result, base.ResolveReference(u).String())
	}

	return
}

func GetSLD(host string) string {
	tokens := strings.Split(host, ".")

	if len(tokens) == 1 {
		return host
	}

	return tokens[len(tokens)-2]
}

func DataFilePath(name string) string {
	_, b, _, ok := runtime.Caller(0)

	if !ok {
		log.Fatalf("failed to get base path for data file")
	}

	basepath := ""

	if flag.Lookup("test.v") != nil {
		basepath = path.Dir(path.Dir(path.Dir(b)))
	} else if p, err := os.Executable(); err != nil {
		log.Fatalf(err.Error())
	} else {
		basepath = path.Dir(p)
	}

	return filepath.Join(basepath, config.DataPath, name)
}
