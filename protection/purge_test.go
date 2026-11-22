package protection

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteResultsDropsRecordsAndReferencesPreservesIntegers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.jsonl")
	input := `{"id":9007199254740993,"message_type":1,"message":{"uri":"http://blocked.test/","features":{"text":"marker"}}}
{"id":9007199254740995,"message_type":1,"message":{"uri":"http://good.test/","origin":"http://blocked.test/","features":{"url":["http://blocked.test/a","http://good.test/b"]}}}
`
	if e := os.WriteFile(path, []byte(input), 0600); e != nil {
		t.Fatal(e)
	}
	match := func(s string) bool { return Entry{Value: "blocked.test", Domain: true}.Matches(s) }
	if e := RewriteJSONL(context.Background(), path, match); e != nil {
		t.Fatal(e)
	}
	out, e := os.ReadFile(path)
	if e != nil || strings.Contains(string(out), "blocked.test") || strings.Contains(string(out), "marker") || !strings.Contains(string(out), "9007199254740995") || !strings.Contains(string(out), "good.test/b") {
		t.Fatal(string(out), e)
	}
	if e = RewriteJSONL(context.Background(), path, match); e != nil {
		t.Fatal(e)
	}
}

func TestInterruptedResultsRewritePreservesOriginalAndCanRetry(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "results.jsonl")
	input := `{"uri":"http://bad.test/","features":{"text":"synthetic"}}
{"uri":"http://good.test/","counter":9007199254740993}
`
	if e := os.WriteFile(path, []byte(input), 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	match := func(s string) bool {
		if strings.Contains(s, "bad.test") {
			cancel()
			return true
		}
		return false
	}
	if e := RewriteJSONL(ctx, path, match); e != context.Canceled {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(path)
	if e != nil || string(raw) != input {
		t.Fatal("interrupted rewrite modified original", e)
	}
	files, e := filepath.Glob(filepath.Join(root, ".purge-*"))
	if e != nil || len(files) != 0 {
		t.Fatal("temporary response copies remain", files, e)
	}
	match = NewMatcher([]Entry{{Domain: true, Value: "bad.test"}}, nil).Matches
	if e = RewriteJSONL(context.Background(), path, match); e != nil {
		t.Fatal(e)
	}
	if e = VerifyJSONL(context.Background(), path, match); e != nil {
		t.Fatal(e)
	}
}

func TestPurgeRemovesRewriteCopyLeftByProcessDeath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "results.jsonl")
	if e := os.WriteFile(path, []byte("{\"uri\":\"http://good.test/\"}\n"), 0600); e != nil {
		t.Fatal(e)
	}
	// A process killed while purging another site can leave a partial copy
	// containing a site that is blocked by a later incident.
	stale, e := os.CreateTemp(root, ".purge-*")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = stale.WriteString("{\"uri\":\"http://blocked.test/\",\"features\":{\"text\":\"synthetic\"}}\n"); e != nil {
		t.Fatal(e)
	}
	if e = stale.Close(); e != nil {
		t.Fatal(e)
	}
	match := NewMatcher([]Entry{{Domain: true, Value: "blocked.test"}}, nil).Matches
	if e = RewriteJSONL(context.Background(), path, match); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(stale.Name()); !os.IsNotExist(e) {
		t.Fatalf("abandoned response copy survived purge: %v", e)
	}
}

func TestRewriteRecoveryIsBoundedAndKeepsOtherDestinations(t *testing.T) {
	for _, kind := range []string{"jsonl", "json", "replace"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "results")
			other := filepath.Join(root, "unrelated")
			keep, e := os.CreateTemp(root, rewriteTempPattern(other))
			if e != nil {
				t.Fatal(e)
			}
			if e := keep.Close(); e != nil {
				t.Fatal(e)
			}
			for n := 0; n < 260; n++ {
				f, e := os.CreateTemp(root, rewriteTempPattern(path))
				if e != nil {
					t.Fatal(e)
				}
				if e := f.Close(); e != nil {
					t.Fatal(e)
				}
			}
			input := []byte("{\"uri\":\"http://good.test/\"}\n")
			if e := os.WriteFile(path, input, 0600); e != nil {
				t.Fatal(e)
			}
			if e := VerifyJSONL(context.Background(), path, func(string) bool { return false }); e == nil {
				t.Fatal("verification accepted an abandoned rewrite copy")
			}
			switch kind {
			case "jsonl":
				e = RewriteJSONL(context.Background(), path, func(string) bool { return false })
			case "json":
				e = RewriteJSONFile(context.Background(), path, func(string) bool { return false })
			case "replace":
				e = ReplaceFile(context.Background(), path, input)
			}
			if e != nil {
				t.Fatal(e)
			}
			if e := verifyRewriteTemps(context.Background(), path); e != nil {
				t.Fatal(e)
			}
			if _, e := os.Stat(keep.Name()); e != nil {
				t.Fatal("another destination's rewrite was removed", e)
			}
		})
	}
}

func TestRewriteRecoveryRejectsUnsupportedTemporaryEntries(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "results.jsonl")
	name := strings.TrimSuffix(rewriteTempPattern(path), "*") + "123"
	if e := os.Mkdir(filepath.Join(root, name), 0700); e != nil {
		t.Fatal(e)
	}
	if e := RewriteJSONL(context.Background(), path, func(string) bool { return false }); e == nil {
		t.Fatal("unsupported temporary entry was silently ignored")
	}
}
