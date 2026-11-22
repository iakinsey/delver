package protection

import (
	"regexp"
	"strings"
)

var locations = regexp.MustCompile(`https?://[^\s<>"'\x00]+`)

// References is for diagnostic metadata, never response classification. Remove
// an entire diagnostic when it contains a blocked location rather than keeping
// a partial excerpt of its original error.
func References(text string, match func(string) bool) bool {
	if match(text) {
		return true
	}
	for len(text) > 0 {
		at := locations.FindStringIndex(text)
		if at == nil {
			break
		}
		if match(strings.TrimRight(text[at[0]:at[1]], ".,);]")) {
			return true
		}
		text = text[at[1]:]
	}
	return false
}
