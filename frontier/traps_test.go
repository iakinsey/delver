package frontier

import (
	"context"
	"embed"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/trap"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
	"math/rand"
	"path/filepath"
	"strings"
	"testing"
)

//go:embed testdata/legitimate/*.html
var legitimateTemplates embed.FS
var legitimateKinds = []string{"wordpress", "forum", "mirror", "directory", "community"}

func corpusObservation(t *testing.T, seed, variant, page, member int, malicious bool) trap.Observation {
	t.Helper()
	rng := rand.New(rand.NewSource(int64(seed)))
	names := []string{}
	for j := 0; j < 3+variant%5; j++ {
		names = append(names, fmt.Sprintf("%x.test", rng.Uint64()))
	}
	uri := fmt.Sprintf("http://s-%d.%s/page/%d", page%32, names[member], page)
	links := []string{}
	linkCount := 12
	if malicious && (variant%4 == 2 || variant%4 == 3) {
		linkCount = 48
	}
	for j := 0; j < linkCount; j++ {
		target := (member + j + 1) % len(names)
		if !malicious && variant%5 == 4 && j < 9 {
			target = member
		}
		path := fmt.Sprintf("/catalog/%d", page*100+j)
		if !malicious {
			if variant%5 == 4 {
				path = fmt.Sprintf("/posts/%d", page*100+j)
			} else if variant%4 == 0 {
				path = fmt.Sprintf("/article/%c%c%c", 'a'+rune(page%26), 'a'+rune(page/26), 'a'+rune(j))
			} else if variant%4 == 1 {
				path = fmt.Sprintf("/navigation/%d", j)
			} else {
				path = fmt.Sprintf("/material/%c%c%c", 'b'+rune(page%24), 'a'+rune(member), 'a'+rune(j))
			}
		}
		links = append(links, fmt.Sprintf("http://s-%d.%s%s", (page+j)%32, names[target], path))
	}
	if !malicious && variant%4 == 1 {
		for j := range links {
			links[j] = fmt.Sprintf("http://%s/navigation/%d", names[j%len(names)], j)
		}
	}
	if malicious && variant%4 == 3 {
		links = append(links, "http://independent.test/outside")
	}
	if malicious && variant%4 == 2 {
		links = append(links, fmt.Sprintf("http://independent-%d-%d.test/outside", page, member))
	}
	var body strings.Builder
	fmt.Fprintf(&body, `<html><head><title>Material %d</title></head><body><header data-id="%d"><nav>Navigation</nav></header><main><article><h1>Content</h1><p>Different words %d</p>`, page, rng.Int(), page*53)
	if variant%2 == 1 && page%3 == 0 {
		body.WriteString("<aside>optional</aside>")
	}
	for _, u := range links {
		fmt.Fprintf(&body, `<a href="%s">Item</a>`, u)
	}
	body.WriteString("</article></main><footer>Footer</footer></body></html>")
	html := body.String()
	if !malicious {
		fixture, e := legitimateTemplates.ReadFile("testdata/legitimate/" + legitimateKinds[variant%len(legitimateKinds)] + ".html")
		require.NoError(t, e)
		var list strings.Builder
		for _, link := range links {
			fmt.Fprintf(&list, `<a data-random="%d" href="%s">Item %d</a>`, rng.Int(), link, page)
		}
		html = strings.ReplaceAll(string(fixture), "{{LINKS}}", list.String())
		html = strings.ReplaceAll(html, "{{TEXT}}", fmt.Sprintf("Independent material %d %d", page, member))
	}
	o, e := trap.Observe(context.Background(), strings.NewReader(html), uri, links)
	require.NoError(t, e)
	return o
}
func TestTrapAcceptanceHeldOutCorpus(t *testing.T) {
	for variant := 0; variant < 20; variant++ {
		var trapResult, legitimateResult []bool
		for rename := 0; rename < 3; rename++ {
			for _, isTrap := range []bool{false, true} {
				name := fmt.Sprintf("network-%02d/rename-%d/trap-%v", variant, rename, isTrap)
				t.Run(name, func(t *testing.T) {
					settings := trap.Default()
					store, e := OpenStore(StoreParams{Path: filepath.Join(t.TempDir(), "frontier"), Traps: &settings})
					require.NoError(t, e)
					defer store.Close()
					blocked := false
					excluded := map[int]bool{}
					members := 3 + variant%5
					observations := 0
					for page := 0; page < 20 && len(excluded) < members; page++ {
						for member := 0; member < members; member++ {
							if excluded[member] {
								continue
							}
							o := corpusObservation(t, 10000+variant*31+rename*137, variant, page, member, isTrap)
							blocked, e = store.ObserveTrap(context.Background(), o)
							require.NoError(t, e)
							observations++
							if blocked {
								excluded[member] = true
							}
						}
					}
					blocked = len(excluded) == members
					decisions, e := store.TrapDecisions(context.Background())
					require.NoError(t, e)
					if isTrap {
						trapResult = append(trapResult, blocked)
						require.True(t, blocked, "trap was not excluded")
						require.LessOrEqual(t, observations, 100)
						require.Equal(t, members, len(decisions))
					} else {
						legitimateResult = append(legitimateResult, blocked)
						require.False(t, blocked, "legitimate network excluded")
						require.Empty(t, decisions)
					}
				})
			}
		}
		require.Equal(t, []bool{true, true, true}, trapResult, "hostname assignment changed trap decisions")
		require.Equal(t, []bool{false, false, false}, legitimateResult, "hostname assignment changed legitimate decisions")
	}
}
func TestTemplateSimilarityAloneDoesNotExclude(t *testing.T) {
	p := trap.Default()
	s, e := OpenStore(StoreParams{Path: filepath.Join(t.TempDir(), "db"), Traps: &p})
	require.NoError(t, e)
	defer s.Close()
	for n := 0; n < 80; n++ {
		o := corpusObservation(t, 9021, 1, n/4, n%4, false)
		blocked, e := s.ObserveTrap(context.Background(), o)
		require.NoError(t, e)
		require.False(t, blocked)
	}
}

func TestTrapCalibrationStructuralVariation(t *testing.T) {
	// Separate calibration documents; the held-out corpus above never supplies
	// the selected thresholds. Renaming/text/attribute changes are not features.
	base := `<html><body><header><nav><a>Menu</a></nav></header><main><section><h1>Heading</h1><p>Material</p></section><section><h2>Second</h2><ul><li><a>Link</a></li></ul></section></main><footer><p>Contact</p></footer></body></html>`
	a, e := trap.Observe(context.Background(), strings.NewReader(base), "http://calibration-a.test/", nil)
	require.NoError(t, e)
	variants := []string{
		strings.ReplaceAll(base, "Material", "Completely changed words"),
		strings.Replace(base, "<section>", `<section id="generated-492" class="different">`, 1),
		strings.Replace(base, "</main>", "<aside>Inserted</aside></main>", 1),
		strings.Replace(base, "<footer><p>Contact</p></footer>", "", 1),
		strings.Replace(base, `<section><h1>Heading</h1><p>Material</p></section><section><h2>Second</h2><ul><li><a>Link</a></li></ul></section>`, `<section><h2>Second</h2><ul><li><a>Link</a></li></ul></section><section><h1>Heading</h1><p>Material</p></section>`, 1),
	}
	for n, html := range variants {
		b, e := trap.Observe(context.Background(), strings.NewReader(html), "http://calibration-b.test/", nil)
		require.NoError(t, e)
		score := trap.Similarity(a.Signature, b.Signature)
		t.Logf("calibration variant=%d similarity=%.5f", n, score)
		require.GreaterOrEqual(t, score, trap.Default().Similarity)
	}
}

func TestTrapParkingFreesCapacityAndRestores(t *testing.T) {
	p := trap.Default()
	path := filepath.Join(t.TempDir(), "db")
	s, e := OpenStore(StoreParams{Path: path, Traps: &p, MaxPending: 2, MaxPendingPerOrigin: 2})
	require.NoError(t, e)
	ctx := context.Background()
	for _, uri := range []string{"http://a.trapped.test/one", "http://b.trapped.test/two"} {
		require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: uri}))
	}
	require.NoError(t, s.db.Update(func(tx *badger.Txn) error {
		return write(tx, "trap/blocked/trapped.test", TrapDecision{Site: "trapped.test"})
	}))
	page, e := s.ScanCandidates(ctx, "breadth_first", ScanCursor{}, 128)
	require.NoError(t, e)
	require.Len(t, page.Trapped, 2)
	require.NoError(t, s.ParkTraps(ctx, page.Trapped))
	counts, e := s.Counts()
	require.NoError(t, e)
	require.Zero(t, counts["pending_and_active"])
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://legitimate.test/page"}))
	require.NoError(t, s.RestoreTrap(ctx, "trapped.test"))
	require.NoError(t, s.DrainParked(ctx, 128))
	counts, e = s.Counts()
	require.NoError(t, e)
	require.EqualValues(t, 2, counts["pending_and_active"])
	require.NoError(t, s.Close())
	s, e = OpenStore(StoreParams{Path: path, Traps: &p, MaxPending: 2, MaxPendingPerOrigin: 2})
	require.NoError(t, e)
	defer s.Close()
	page, e = s.ScanCandidates(ctx, "breadth_first", ScanCursor{}, 128)
	require.NoError(t, e)
	require.Empty(t, page.Trapped)
	require.Len(t, page.Candidates, 2)
}

func TestTrapFilledFrontierDoesNotStarveUnrelatedRunnableSite(t *testing.T) {
	p := trap.Default()
	s, e := OpenStore(StoreParams{Path: filepath.Join(t.TempDir(), "frontier"), Traps: &p, MaxPending: 1100})
	require.NoError(t, e)
	defer s.Close()
	ctx := context.Background()
	for n := 0; n < 1000; n++ {
		require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: fmt.Sprintf("http://s-%d.trapped.test/%d", n, n)}))
	}
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://unrelated.test/page"}))
	require.NoError(t, s.db.Update(func(tx *badger.Txn) error {
		return write(tx, "trap/blocked/trapped.test", TrapDecision{Site: "trapped.test"})
	}))
	cursor := ScanCursor{}
	seen := false
	for pass := 0; pass < 20; pass++ {
		page, e := s.ScanCandidates(ctx, "breadth_first", cursor, 64)
		require.NoError(t, e)
		require.LessOrEqual(t, page.Inspected, 64)
		require.NoError(t, s.ParkTraps(ctx, page.Trapped))
		cursor = page.Cursor
		for _, c := range page.Candidates {
			if c.Request.URI == "http://unrelated.test/page" {
				seen = true
			}
		}
		if seen {
			break
		}
	}
	require.True(t, seen, "blocked traps stalled runnable work")
	counts, e := s.Counts()
	require.NoError(t, e)
	require.EqualValues(t, 1000, counts["trap_parked"])
	require.EqualValues(t, 1, counts["pending_and_active"])
}

func TestDetectorSettingsDoNotSilentlyRestoreExistingExclusions(t *testing.T) {
	p := trap.Default()
	path := filepath.Join(t.TempDir(), "db")
	s, e := OpenStore(StoreParams{Path: path, Traps: &p})
	require.NoError(t, e)
	require.NoError(t, s.db.Update(func(tx *badger.Txn) error {
		return write(tx, "trap/blocked/trapped.test", TrapDecision{Site: "trapped.test"})
	}))
	require.NoError(t, s.Close())
	p.Enabled = false
	p.Automatic = false
	s, e = OpenStore(StoreParams{Path: path, Traps: &p})
	require.NoError(t, e)
	defer s.Close()
	require.NoError(t, s.db.View(func(tx *badger.Txn) error { require.True(t, s.trapBlocked(tx, "http://sub.trapped.test/")); return nil }))
	require.NoError(t, s.RestoreTrap(context.Background(), "trapped.test"))
	require.NoError(t, s.RestoreTrap(context.Background(), "trapped.test"))
	require.NoError(t, s.db.View(func(tx *badger.Txn) error {
		require.False(t, s.trapBlocked(tx, "http://sub.trapped.test/"))
		return nil
	}))
}

func TestTrapExclusionRetainsStagedDiscoveriesAcrossRestartAndRestore(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "frontier")
	params := StoreParams{Path: root, MaxPending: 10, MaxPendingPerOrigin: 10}
	store, e := OpenStore(params)
	require.NoError(t, e)
	defer func() { store.Close() }()
	requests := []message.FetcherRequest{{URI: "http://blocked.test/a"}, {URI: "http://blocked.test/b"}, {URI: "http://unrelated.test/ready"}}
	require.NoError(t, store.StageDiscovery(ctx, "before-exclusion", requests))
	require.NoError(t, store.db.Update(func(tx *badger.Txn) error {
		return write(tx, "trap/blocked/blocked.test", TrapDecision{Site: "blocked.test"})
	}))
	for i := 0; i < 4; i++ {
		_, e = store.DrainDiscovery(ctx, 1)
		require.NoError(t, e)
	}
	counts, e := store.Counts()
	require.NoError(t, e)
	require.Zero(t, counts["deferred_discoveries"])
	require.EqualValues(t, 2, counts["trap_parked"])
	require.EqualValues(t, 1, counts["pending_and_active"])
	require.NoError(t, store.Close())
	store, e = OpenStore(params)
	require.NoError(t, e)
	_, e = store.DrainDiscovery(ctx, 10)
	require.NoError(t, e)
	counts, e = store.Counts()
	require.NoError(t, e)
	require.Zero(t, counts["deferred_discoveries"])
	require.EqualValues(t, 2, counts["trap_parked"])
	require.NoError(t, store.RestoreTrap(ctx, "blocked.test"))
	for i := 0; i < 4; i++ {
		require.NoError(t, store.DrainParked(ctx, 2))
		_, e = store.DrainDiscovery(ctx, 2)
		require.NoError(t, e)
	}
	counts, e = store.Counts()
	require.NoError(t, e)
	require.Zero(t, counts["deferred_discoveries"])
	require.EqualValues(t, 3, counts["pending_and_active"])
	require.Zero(t, counts["trap_parked"])
}
