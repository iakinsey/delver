package frontier

import (
	"log"
	"regexp"
)

type regexFilter struct {
	pattern *regexp.Regexp
}

func NewRegexFilter(pattern string) Filter {
	patternRegexp, err := regexp.Compile(pattern)

	if err != nil {
		log.Fatalf("failed to compile pattern: %s", err)
	}

	return &regexFilter{
		pattern: patternRegexp,
	}
}

func (s *regexFilter) IsAllowed(u string) (bool, error) {
	return s.pattern.Match([]byte(u)), nil
}
