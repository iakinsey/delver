package protection

import (
	"os"
	"testing"
)

func TestPublicThreatFeedFormat(t *testing.T) {
	path := os.Getenv("DELVER_PROTECTION_FEED_FILE")
	if path == "" {
		t.Skip("set DELVER_PROTECTION_FEED_FILE to the downloaded public preset")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	entries, e := ParseList(string(raw))
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) < 1000 {
		t.Fatal("public preset unexpectedly small", len(entries))
	}
	for _, entry := range entries {
		if !entry.Domain {
			t.Fatal("domain-format preset contains URL entries")
		}
	}
	t.Logf("validated domain-format threat feed: bytes=%d entries=%d", len(raw), len(entries))
}
