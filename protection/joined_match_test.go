package protection

import (
	"context"
	"github.com/stretchr/testify/require"
	"math/rand"
	"strings"
	"testing"
)

func TestJoinedMatchesConcatenatedReference(t *testing.T) {
	r := rand.New(rand.NewSource(721))
	parts := []string{"alpha", "beta", " ", "  ", "界", "é", "\xff", "_", "1", "\t"}
	for i := 0; i < 10000; i++ {
		makeText := func() string {
			var s strings.Builder
			for j, n := 0, r.Intn(12); j < n; j++ {
				s.WriteString(parts[r.Intn(len(parts))])
			}
			return s.String()
		}
		left, right, phrase := makeText(), makeText(), makeText()
		joined := left + " " + right
		if i%2 == 0 && len(joined) > 0 {
			a := r.Intn(len(joined))
			phrase = joined[a : a+r.Intn(len(joined)-a+1)]
		}
		require.Equal(t, phraseMatches(joined, phrase), phraseMatchesJoined(left, right, phrase), "%q %q %q", left, right, phrase)
	}
}
func FuzzJoinedMatchesReference(f *testing.F) {
	f.Add("some alpha", "beta words", "alpha beta")
	f.Add("éalpha", "beta界", "alpha beta")
	f.Fuzz(func(t *testing.T, left, right, phrase string) {
		if len(left)+len(right)+len(phrase) > 65536 {
			return
		}
		require.Equal(t, phraseMatches(left+" "+right, phrase), phraseMatchesJoined(left, right, phrase))
	})
}
func TestInspectorObservesRuleRevision(t *testing.T) {
	p, err := Open(t.TempDir() + "/policy.sqlite")
	require.NoError(t, err)
	defer p.Close()
	require.NoError(t, p.Update(1, nil, nil))
	r := p.InspectReader(context.Background(), nil, "http://fixture.invalid/page", "crawl", "run", 0)
	defer r.Close()
	require.NoError(t, r.InspectChunk([]byte("synthetic restricted ")))
	require.NoError(t, p.Update(2, nil, []Rule{{ID: "fixture", Enabled: true, Phrases: []string{"synthetic restricted marker"}}}))
	require.ErrorIs(t, r.InspectChunk([]byte("marker")), ErrBlocked)
}
