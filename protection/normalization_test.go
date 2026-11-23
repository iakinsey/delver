package protection

import (
	"html"
	"math/rand"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

func legacyNormalizedText(s string) string {
	s = tags.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = strings.ToLower(norm.NFKC.String(s))
	return strings.Join(strings.Fields(s), " ")
}

func checkNormalization(t *testing.T, s string) {
	t.Helper()
	if got, want := normalizedWhitespace(s), strings.Join(strings.Fields(s), " "); got != want {
		t.Fatalf("whitespace normalization differs: input=%q got=%q want=%q", s, got, want)
	}
	if got, want := normalizedText(s), legacyNormalizedText(s); got != want {
		t.Fatalf("content normalization differs: input=%q got=%q want=%q", s, got, want)
	}
}

func TestNormalizationMatchesLegacyUnicodeAndHTML(t *testing.T) {
	for _, s := range []string{"", "single spaced text", " \t\n text \r\n\t", "<p>synthetic</p>restricted&nbsp;marker", "ＳＹＮＴＨＥＴＩＣ\u00a0restricted\u2003marker", "e\u0301 \u2028 A", "\xff\xfe\t\xff\n", "<invalid\xff>abc"} {
		checkNormalization(t, s)
	}
	for value := rune(0); value <= 255; value++ {
		checkNormalization(t, "A"+string(value)+"B")
	}
	random := rand.New(rand.NewSource(72325))
	parts := []string{"word", "A", " ", "\t", "\r\n", "\u0085", "\u00a0", "\u2000", "\u2003", "\u2028", "\u2029", "\u202f", "\u205f", "\u3000", "\u200b", "界", "É", "e\u0301", "<p>", "</p>", "&amp;", "&#32;", "\xff", "\xfe"}
	for i := 0; i < 5000; i++ {
		var input strings.Builder
		for j, n := 0, random.Intn(200); j < n; j++ {
			input.WriteString(parts[random.Intn(len(parts))])
		}
		checkNormalization(t, input.String())
	}
}

func FuzzNormalizationMatchesLegacy(f *testing.F) {
	for _, s := range []string{"single spaced text", "\tA\u2003B ", "<p>synthetic</p>restricted&nbsp;marker", "\xff \xfe\u3000"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) <= 65536 {
			checkNormalization(t, s)
		}
	})
}
