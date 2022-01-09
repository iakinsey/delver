package util

import (
	"bufio"
	"net/url"
	"os"
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