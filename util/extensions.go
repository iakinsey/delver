package util

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

var ErrFileExtensionExcluded = errors.New("file extension excluded by crawl policy")

// NormalizeExtensions accepts comma-separated suffixes, including compound ones.
func NormalizeExtensions(input string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, part := range strings.Split(input, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		part = strings.TrimPrefix(strings.ToLower(part), ".")
		for _, segment := range strings.Split(part, ".") {
			if segment == "" {
				return nil, fmt.Errorf("invalid excluded file extension %q", part)
			}
			for _, r := range segment {
				if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
					return nil, fmt.Errorf("invalid excluded file extension %q", part)
				}
			}
		}
		part = "." + part
		if !seen[part] {
			out = append(out, part)
			seen[part] = true
		}
	}
	return out, nil
}
func ExtensionExcluded(u *url.URL, extensions []string) bool {
	if u == nil {
		return false
	}
	// url.Parse already decodes Path exactly once. Never decode a second time.
	path := strings.ToLower(u.Path)
	name := path[strings.LastIndex(path, "/")+1:]
	for _, extension := range extensions {
		if strings.HasSuffix(name, extension) {
			return true
		}
	}
	return false
}
func URLFileExcluded(raw string, extensions []string) bool {
	u, err := url.Parse(raw)
	return err == nil && ExtensionExcluded(u, extensions)
}
