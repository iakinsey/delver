package frontier

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/report"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/repetition"
	"github.com/iakinsey/delver/trap"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
)

func repeatedFixture(host, body string, event int) (message.CompositeAnalysis, []string) {
	uri := "http://" + host + "/"
	signature := make(trap.HashVector, 64)
	for i := range signature {
		signature[i] = uint64(i + 1)
	}
	links := []string{}
	for i := 0; i < 20; i++ {
		links = append(links, fmt.Sprintf("http://peer-%d.test/resource", i))
	}
	return message.CompositeAnalysis{FetcherResponse: message.FetcherResponse{FetcherRequest: message.FetcherRequest{URI: uri, RequestID: types.UUID(fmt.Sprintf("event-%s-%d", host, event)), QueuedAt: int64(event)}, EffectiveURI: uri, Success: true, HTTPCode: 200, Timestamp: int64(event)}, Features: map[string]interface{}{features.TextField: body + " " + uri, features.AdversarialField: features.Adversarial{Structure: &trap.Observation{URI: uri, Site: host, Signature: signature, Tokens: 80}}}}, links
}
func repeatedBody(seed string) string {
	words := []string{}
	for i := 0; i < 200; i++ {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", seed, i)))
		words = append(words, fmt.Sprintf("%x", h[:6]))
	}
	return strings.Join(words, " ")
}
func repeatedTestStore(t *testing.T, p repetition.Settings) *Store {
	t.Helper()
	s, e := OpenStore(StoreParams{Path: t.TempDir(), RepeatedPages: &p, MaxPending: 2000})
	require.NoError(t, e)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return s
}
func repeatedActive(t *testing.T, s *Store, host string) (repeatedSite, bool) {
	t.Helper()
	var h repeatedSite
	active := false
	require.NoError(t, s.view(func(tx *badger.Txn) error {
		if e := read(tx, "repeated/site/"+host, &h); e != nil {
			return e
		}
		var e error
		active, e = s.repeatedFamilyActive(tx, h.Family)
		return e
	}))
	return h, active
}
func confirmRepeatedFixture(t *testing.T, s *Store) string {
	t.Helper()
	for i := 0; i < 3; i++ {
		c, links := repeatedFixture(fmt.Sprintf("mirror-%d.test", i), repeatedBody("shared"), 1)
		require.NoError(t, s.AnalyzeRepeated(context.Background(), string(c.RequestID), c, links, 0))
	}
	h, active := repeatedActive(t, s, "mirror-0.test")
	require.True(t, active)
	return h.Family
}

func TestRepeatedFamiliesHeldOutCorpusAndRenaming(t *testing.T) {
	// Twenty replicated networks and twenty independent-content networks, each
	// under three unrelated hostname assignments. Structure and navigation are
	// deliberately identical in the controls; text must distinguish them.
	for rename := 0; rename < 3; rename++ {
		t.Run(fmt.Sprint(rename), func(t *testing.T) {
			p := repetition.Default()
			p.Mode = "observe"
			s := repeatedTestStore(t, p)
			detected, total := 0, 0
			for network := 0; network < 20; network++ {
				for member := 0; member < 3; member++ {
					host := fmt.Sprintf("assignment-%d-network-%d-member-%d.test", rename, network, member)
					c, links := repeatedFixture(host, repeatedBody(fmt.Sprintf("replicated-%d", network))+fmt.Sprintf(" item %d", member), 1)
					require.NoError(t, s.AnalyzeRepeated(context.Background(), string(c.RequestID), c, links, 0))
				}
				for member := 0; member < 3; member++ {
					host := fmt.Sprintf("assignment-%d-network-%d-member-%d.test", rename, network, member)
					_, active := repeatedActive(t, s, host)
					if active {
						detected++
					}
					total++
				}
			}
			for network := 0; network < 20; network++ {
				for member := 0; member < 3; member++ {
					host := fmt.Sprintf("control-%d-network-%d-member-%d.test", rename, network, member)
					c, links := repeatedFixture(host, repeatedBody(fmt.Sprintf("independent-%d-%d", network, member)), 1)
					require.NoError(t, s.AnalyzeRepeated(context.Background(), string(c.RequestID), c, links, 0))
					_, active := repeatedActive(t, s, host)
					require.False(t, active, "independent material was classified")
				}
			}
			require.GreaterOrEqual(t, float64(detected)/float64(total), .95)
			decisions, e := s.TrapDecisions(context.Background())
			require.NoError(t, e)
			require.Empty(t, decisions, "repetition never creates trap exclusions")
			t.Logf("replicated=%d/%d independent controls=60 excluded=0", detected, total)
		})
	}
}

func TestRepeatedFamilyReplayNoveltyExemptionAndRestart(t *testing.T) {
	p := repetition.Default()
	path := t.TempDir()
	params := StoreParams{Path: path, RepeatedPages: &p}
	s, e := OpenStore(params)
	require.NoError(t, e)
	id := confirmRepeatedFixture(t, s)
	c, links := repeatedFixture("mirror-0.test", repeatedBody("shared"), 1)
	require.NoError(t, s.AnalyzeRepeated(context.Background(), string(c.RequestID), c, links, 0))
	h, active := repeatedActive(t, s, "mirror-0.test")
	require.True(t, active)
	require.Equal(t, 1, h.Samples)
	request := message.FetcherRequest{URI: "http://target.test/repeated", Origin: c.URI}
	require.NoError(t, s.Admit(context.Background(), request))
	var pending Candidate
	require.NoError(t, s.view(func(tx *badger.Txn) error { pending = Candidate{}; return read(tx, "u/"+request.URI, &pending) }))
	require.True(t, pending.Repeated)
	// Independent provenance promotes an existing URL even without a better path.
	normal := request
	normal.Origin = "http://independent.test/"
	independent, independentLinks := repeatedFixture("independent.test", repeatedBody("independent material"), 1)
	require.NoError(t, s.AnalyzeRepeated(context.Background(), string(independent.RequestID), independent, independentLinks, 0))
	require.NoError(t, s.Admit(context.Background(), normal))
	require.NoError(t, s.view(func(tx *badger.Txn) error { pending = Candidate{}; return read(tx, "u/"+request.URI, &pending) }))
	require.False(t, pending.Repeated)
	require.Len(t, pending.Origins, 2)
	c, links = repeatedFixture("mirror-0.test", repeatedBody("distinct material"), 2)
	require.NoError(t, s.AnalyzeRepeated(context.Background(), string(c.RequestID), c, links, 0))
	h, active = repeatedActive(t, s, "mirror-0.test")
	require.False(t, active, "mixed-content host must unfold")
	require.Empty(t, h.Family)
	require.NoError(t, s.SetFamilyExempt(context.Background(), id, true))
	_, active = repeatedActive(t, s, "mirror-1.test")
	require.False(t, active)
	require.NoError(t, s.Close())
	s, e = OpenStore(params)
	require.NoError(t, e)
	defer s.Close()
	_, active = repeatedActive(t, s, "mirror-1.test")
	require.False(t, active, "exemption lost across restart")
	require.NoError(t, s.SetFamilyExempt(context.Background(), id, false))
	_, active = repeatedActive(t, s, "mirror-1.test")
	require.True(t, active)
}

func TestRepeatedProfileBoundsSubdomainsAndLiveEpoch(t *testing.T) {
	p := repetition.Default()
	s := repeatedTestStore(t, p)
	for i := 0; i < 3; i++ {
		c, links := repeatedFixture(fmt.Sprintf("sub-%d.one.test", i), repeatedBody("shared"), 1)
		require.NoError(t, s.AnalyzeRepeated(context.Background(), string(c.RequestID), c, links, 0))
	}
	_, active := repeatedActive(t, s, "one.test")
	require.False(t, active, "subdomains must not supply independent-site evidence")
	c, links := repeatedFixture("bounded.test", strings.Repeat("x", (256<<10)+1), 1)
	_, reason := repetition.Build(c, links, p)
	require.Equal(t, "text_limit", reason)
	c, links = repeatedFixture("bounded.test", repeatedBody("bounded"), 1)
	for i := 0; i < 257; i++ {
		links = append(links, fmt.Sprintf("http://destination-%d.test/", i))
	}
	_, reason = repetition.Build(c, links, p)
	require.Equal(t, "destination_limit", reason)
	c, links = repeatedFixture("fresh.test", repeatedBody("current live content"), 4)
	require.NoError(t, s.AnalyzeRepeated(context.Background(), string(c.RequestID), c, links, 0))
	h, _ := repeatedActive(t, s, "fresh.test")
	original := h.Family
	c, links = repeatedFixture("fresh.test", repeatedBody("stale historical content"), 3)
	require.NoError(t, s.AnalyzeRepeated(context.Background(), string(c.RequestID), c, links, h.LastLive-1))
	h, _ = repeatedActive(t, s, "fresh.test")
	require.Equal(t, original, h.Family)
}

func maintainRepeated(t *testing.T, s *Store) {
	t.Helper()
	for i := 0; i < 100000; i++ {
		v, e := s.RepeatedPages(context.Background(), "", "", 1)
		require.NoError(t, e)
		if v.Routing.Phase == "" {
			return
		}
		require.NoError(t, s.MaintainRepeatedRouting(context.Background(), 128))
	}
	t.Fatal("routing did not converge")
}
func TestRepeatedStagingCapacityPrefixAndDeletedCursors(t *testing.T) {
	ctx := context.Background()
	s := repeatedTestStore(t, repetition.Default())
	s.params.MaxPendingPerOrigin = 1
	confirmRepeatedFixture(t, s)
	maintainRepeated(t, s)
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://full.test/seed"}))
	requests := []message.FetcherRequest{}
	for i := 0; i < 400; i++ {
		requests = append(requests, message.FetcherRequest{URI: fmt.Sprintf("http://full.test/%d", i), Origin: "http://independent.test/"})
	}
	requests = append(requests, message.FetcherRequest{URI: "http://available.test/", Origin: "http://mirror-0.test/"})
	require.NoError(t, s.StageDiscovery(ctx, "capacity", requests))
	for i := 0; i < 100; i++ {
		_, e := s.DrainDiscovery(ctx, 7)
		require.NoError(t, e)
	}
	require.NoError(t, s.view(func(tx *badger.Txn) error {
		var c Candidate
		require.NoError(t, read(tx, "u/http://available.test/", &c))
		require.True(t, c.Repeated)
		return nil
	}))
	// A new normal source in the same chunk must also be found after deferred
	// entries, even when the previous cursor's payload/chunk was deleted.
	require.NoError(t, s.StageDiscovery(ctx, "later", []message.FetcherRequest{{URI: "http://later.test/", Origin: "http://independent.test/"}}))
	for i := 0; i < 100; i++ {
		_, e := s.DrainDiscovery(ctx, 7)
		require.NoError(t, e)
	}
	require.NoError(t, s.view(func(tx *badger.Txn) error {
		var c Candidate
		require.NoError(t, read(tx, "u/http://later.test/", &c))
		require.False(t, c.Repeated)
		return nil
	}))
}
func TestRepeatedRoutingOrderingBorrowingAndDowngradeRestoration(t *testing.T) {
	for _, strategy := range []string{"breadth_first", "depth_first", "host_diverse", "adaptive"} {
		t.Run(strategy, func(t *testing.T) {
			ctx := context.Background()
			s := repeatedTestStore(t, repetition.Default())
			confirmRepeatedFixture(t, s)
			maintainRepeated(t, s)
			for i := 0; i < 100; i++ {
				require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: fmt.Sprintf("http://normal-%d.test/", i), Origin: "http://independent.test/", Depth: i % 4, Hops: 1}))
				require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: fmt.Sprintf("http://repeat-%d.test/", i), Origin: "http://mirror-0.test/", Depth: i % 4, Hops: 1}))
			}
			page, e := s.ScanCandidates(ctx, strategy, ScanCursor{}, 100)
			require.NoError(t, e)
			require.Len(t, page.Candidates, 100)
			repeated := 0
			for _, c := range page.Candidates {
				if c.Repeated {
					repeated++
				}
			}
			require.Equal(t, 10, repeated)
			// A wider sweep must borrow all idle quota without dropping either lane.
			page, e = s.ScanCandidates(ctx, strategy, ScanCursor{}, 500)
			require.NoError(t, e)
			require.Len(t, page.Candidates, 200)
			require.True(t, page.Complete)
			params := s.params
			require.NoError(t, s.Close())
			off := repetition.Default()
			off.Mode = "off"
			params.RepeatedPages = &off
			reopened, e := OpenStore(params)
			require.NoError(t, e)
			defer reopened.Close()
			maintainRepeated(t, reopened)
			require.NoError(t, reopened.view(func(tx *badger.Txn) error {
				it := tx.NewIterator(badger.DefaultIteratorOptions)
				defer it.Close()
				for _, prefix := range []string{"rb/", "rd/", "rhs/", "frb/", "frd/", "frhs/"} {
					it.Seek([]byte(prefix))
					require.False(t, it.ValidForPrefix([]byte(prefix)), "legacy downgrade left priority index %s", prefix)
				}
				return nil
			}))
			// No feature settings emulates the previous binary's index selection.
			legacy, e := reopened.scanLegacyCandidates(ctx, strategy, ScanCursor{}, 500)
			require.NoError(t, e)
			require.Len(t, legacy.Candidates, 200)
		})
	}
}

func TestRepeatedHeldOutHTMLVariantsAndIndependentControls(t *testing.T) {
	// Held-out topology/content assignments are separate from the small default
	// calibration fixture. Existing trap corpus and thresholds are unchanged.
	for rename := 0; rename < 3; rename++ {
		t.Run(fmt.Sprint(rename), func(t *testing.T) {
			ctx := context.Background()
			s := repeatedTestStore(t, repetition.Default())
			detected := 0
			for network := 0; network < 20; network++ {
				hosts := []string{}
				for member := 0; member < 3; member++ {
					host := fmt.Sprintf("html-%d-%d-%d.test", rename, network, member)
					hosts = append(hosts, host)
					c, links := repeatedFixture(host, repeatedBody(fmt.Sprintf("html-shared-%d", network)), 1)
					sectionA := `<section class="first"><h2>First</h2><p><strong>Entry</strong></p><ul><li><a>Resource</a></li></ul></section>`
					sectionB := `<section class="second"><h2>Second</h2><p><em>Content</em></p><ol><li><a>Other</a></li></ol></section>`
					markup := `<html><head><title>Page</title></head><body><header><nav><a>Index</a></nav></header><main><article>`
					if member == 1 {
						markup += sectionB + sectionA + sectionA
					} else {
						markup += sectionA + sectionB
					}
					markup += `</article></main><footer><p>End</p></footer></body></html>`
					// Random IDs and attribute ordering do not change structural evidence.
					markup = strings.ReplaceAll(markup, `class="first"`, fmt.Sprintf(`data-n="%d" id="generated-%d-%d" class="first"`, member, rename, network))
					observation, e := trap.Observe(ctx, strings.NewReader(markup), c.URI, links)
					require.NoError(t, e)
					c.Features[features.AdversarialField] = features.Adversarial{Structure: &observation}
					if member == 2 {
						links = append(links, "http://decorative-outward.test/")
					}
					require.NoError(t, s.AnalyzeRepeated(ctx, string(c.RequestID), c, links, 0))
				}
				for _, host := range hosts {
					_, active := repeatedActive(t, s, host)
					if active {
						detected++
					}
				}
				// Shared CMS, shared navigation and an enclosed graph are insufficient:
				// substantive independent articles retain normal classification.
				for member := 0; member < 3; member++ {
					host := fmt.Sprintf("control-%d-%d-%d.test", rename, network, member)
					body := "Common navigation archive contact sitemap " + repeatedBody(fmt.Sprintf("independent-article-%d-%d", network, member))
					c, links := repeatedFixture(host, body, 1)
					require.NoError(t, s.AnalyzeRepeated(ctx, string(c.RequestID), c, links, 0))
					_, active := repeatedActive(t, s, host)
					require.False(t, active)
				}
			}
			require.GreaterOrEqual(t, detected, 57, "held-out replicated-member recall below 95%%")
		})
	}
}
func TestRepeatedPrototypeWindowAndHostRestoration(t *testing.T) {
	ctx := context.Background()
	s := repeatedTestStore(t, repetition.Default())
	confirmRepeatedFixture(t, s)
	maintainRepeated(t, s)
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://pending.test/", Origin: "http://mirror-0.test/"}))
	for i := 2; i < 32; i++ {
		c, links := repeatedFixture("mirror-0.test", repeatedBody(fmt.Sprint(i)), i)
		require.NoError(t, s.AnalyzeRepeated(ctx, string(c.RequestID), c, links, 0))
	}
	maintainRepeated(t, s)
	require.NoError(t, s.view(func(tx *badger.Txn) error {
		var c Candidate
		require.NoError(t, read(tx, "u/http://pending.test/", &c))
		require.False(t, c.Repeated)
		var h repeatedSite
		require.NoError(t, read(tx, "repeated/site/mirror-0.test", &h))
		require.Len(t, h.Window, 20)
		require.Empty(t, h.Family)
		n := 0
		it := tx.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek([]byte("repeated/family/")); it.ValidForPrefix([]byte("repeated/family/")); it.Next() {
			n++
		}
		require.LessOrEqual(t, n, 21)
		return nil
	}))
}

func TestRepeatedStagingDuplicatesBorrowWithoutPrefixStarvation(t *testing.T) {
	ctx := context.Background()
	s := repeatedTestStore(t, repetition.Default())
	confirmRepeatedFixture(t, s)
	maintainRepeated(t, s)
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://known.test/"}))
	requests := []message.FetcherRequest{}
	for i := 0; i < 2000; i++ {
		requests = append(requests, message.FetcherRequest{URI: "http://known.test/"})
	}
	requests = append(requests, message.FetcherRequest{URI: "http://new.test/", Origin: "http://mirror-0.test/"})
	require.NoError(t, s.StageDiscovery(ctx, "duplicates", requests))
	for i := 0; i < 20; i++ {
		_, err := s.DrainDiscovery(ctx, 7)
		require.NoError(t, err)
	}
	require.NoError(t, s.view(func(tx *badger.Txn) error {
		var c Candidate
		require.NoError(t, read(tx, "u/http://new.test/", &c))
		require.True(t, c.Repeated)
		return nil
	}))
	counts, err := s.Counts()
	require.NoError(t, err)
	require.Greater(t, counts["deferred_discoveries"], int64(1000), "fixture must still contain its duplicate-heavy prefix")
}

func TestRepeatedRecrawlProvenanceSurvivesInterruptedAdmission(t *testing.T) {
	ctx := context.Background()
	s := repeatedTestStore(t, repetition.Default())
	confirmRepeatedFixture(t, s)
	s.params.RevisitAfter = Lifetime(time.Nanosecond)
	independent, links := repeatedFixture("independent.test", repeatedBody("independent"), 1)
	require.NoError(t, s.AnalyzeRepeated(ctx, string(independent.RequestID), independent, links, 0))
	request := message.FetcherRequest{URI: "http://revisit.test/", Origin: "http://mirror-0.test/"}
	require.NoError(t, s.Admit(ctx, request))
	request.Origin = independent.URI
	require.NoError(t, s.Admit(ctx, request))
	var c Candidate
	require.NoError(t, s.view(func(tx *badger.Txn) error { return read(tx, "u/"+request.URI, &c) }))
	require.False(t, c.Repeated)
	require.Len(t, c.Origins, 2)
	c, err := s.Reserve(ctx, c)
	require.NoError(t, err)
	require.NoError(t, s.Published(c))
	require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: c.Request, Success: true, HTTPCode: 200}))
	// Reproduce process death after admitting the replacement but before copying
	// its provenance. The still-durable revisit receipt must repair the new ID.
	require.NoError(t, s.update(func(tx *badger.Txn) error { return tx.Delete([]byte("u/" + request.URI)) }))
	replacement := c.Request
	replacement.RequestID = ""
	replacement.FrontierGeneration = ""
	require.NoError(t, s.Admit(ctx, replacement))
	require.NoError(t, s.DrainRecrawls(ctx, 128))
	c = Candidate{}
	require.NoError(t, s.view(func(tx *badger.Txn) error { return read(tx, "u/"+request.URI, &c) }))
	require.False(t, c.Repeated)
	require.Len(t, c.Origins, 2)
}

func TestRepeatedProtectionPurgesMetadataAndFencesAnalysis(t *testing.T) {
	ctx := context.Background()
	s := repeatedTestStore(t, repetition.Default())
	confirmRepeatedFixture(t, s)
	ledger, err := report.Open(filepath.Join(t.TempDir(), "report.sqlite"), false)
	require.NoError(t, err)
	defer ledger.Close()
	require.NoError(t, s.FlushRepeated(ctx, ledger))
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://unrelated.test/", Origin: "http://mirror-0.test/", ScopeRoot: "http://mirror-0.test/"}))
	policy, err := protection.Open(filepath.Join(t.TempDir(), "policy.sqlite"))
	require.NoError(t, err)
	defer policy.Close()
	_, err = policy.Trigger("http://mirror-0.test/", "synthetic", "crawl", "run", 0)
	require.ErrorIs(t, err, protection.ErrBlocked)
	s.SetProtection(policy)
	ledger.SetProtection(policy)
	match := protection.NewMatcher(nil, []protection.Target{{Site: "mirror-0.test"}}).Matches
	require.NoError(t, s.PurgeMatching(ctx, match))
	require.NoError(t, s.VerifyNoMatches(ctx, match))
	require.NoError(t, ledger.PurgeSite(ctx, "mirror-0.test"))
	require.NoError(t, s.FlushRepeated(ctx, ledger))
	c, links := repeatedFixture("mirror-0.test", repeatedBody("shared"), 2)
	require.ErrorIs(t, s.AnalyzeRepeated(ctx, string(c.RequestID), c, links, 0), protection.ErrBlocked)
	require.NoError(t, s.VerifyNoMatches(ctx, match))
	require.NoError(t, ledger.VerifyPurged(ctx, nil, []protection.Target{{Site: "mirror-0.test"}}))
	maintainRepeated(t, s)
	var pending Candidate
	require.NoError(t, s.view(func(tx *badger.Txn) error { return read(tx, "u/http://unrelated.test/", &pending) }))
	require.False(t, pending.Repeated)
}

func TestRepeatedDispatchSharePhaseSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	p := repetition.Default()
	p.AdmissionShare = 1
	params := StoreParams{Path: t.TempDir(), RepeatedPages: &p}
	s, err := OpenStore(params)
	require.NoError(t, err)
	confirmRepeatedFixture(t, s)
	maintainRepeated(t, s)
	for i := 0; i < 99; i++ {
		uri := fmt.Sprintf("http://phase-%d.test/", i)
		require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: uri}))
		var c Candidate
		require.NoError(t, s.view(func(tx *badger.Txn) error { return read(tx, "u/"+uri, &c) }))
		c, err = s.Reserve(ctx, c)
		require.NoError(t, err)
		require.NoError(t, s.Published(c))
		require.NoError(t, s.Outcome(message.FetcherResponse{FetcherRequest: c.Request, Success: true, HTTPCode: 200}))
	}
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://normal-next.test/"}))
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: "http://repeated-next.test/", Origin: "http://mirror-0.test/"}))
	require.NoError(t, s.Close())
	s, err = OpenStore(params)
	require.NoError(t, err)
	defer s.Close()
	step, err := s.RepeatedDispatchStep()
	require.NoError(t, err)
	require.EqualValues(t, 99, step)
	page, err := s.ScanCandidates(ctx, "breadth_first", ScanCursor{}, 1)
	require.NoError(t, err)
	require.Len(t, page.Candidates, 1)
	require.Equal(t, "http://repeated-next.test/", page.Candidates[0].Request.URI)
}

func TestRepeatedHeldOutInterleavedFamilies(t *testing.T) {
	ctx := context.Background()
	s := repeatedTestStore(t, repetition.Default())
	for network := 0; network < 20; network++ {
		c, links := repeatedFixture(fmt.Sprintf("interleaved-%d-0.test", network), repeatedBody(fmt.Sprintf("temporal-%d", network))+" edition 0", 1)
		require.NoError(t, s.AnalyzeRepeated(ctx, string(c.RequestID), c, links, 0))
	}
	for n := 0; n < 60; n++ {
		c, links := repeatedFixture(fmt.Sprintf("independent-interleaving-%d.test", n), repeatedBody(fmt.Sprintf("independent-temporal-%d", n)), 1)
		require.NoError(t, s.AnalyzeRepeated(ctx, string(c.RequestID), c, links, 0))
	}
	for member := 1; member < 3; member++ {
		for network := 0; network < 20; network++ {
			c, links := repeatedFixture(fmt.Sprintf("interleaved-%d-%d.test", network, member), repeatedBody(fmt.Sprintf("temporal-%d", network))+fmt.Sprintf(" edition %d", member), 1)
			require.NoError(t, s.AnalyzeRepeated(ctx, string(c.RequestID), c, links, 0))
		}
	}
	detected := 0
	for network := 0; network < 20; network++ {
		for member := 0; member < 3; member++ {
			_, active := repeatedActive(t, s, fmt.Sprintf("interleaved-%d-%d.test", network, member))
			if active {
				detected++
			}
		}
	}
	require.GreaterOrEqual(t, detected, 57, "interleaved eligible members fell below 95%% recall")
	for n := 0; n < 60; n++ {
		_, active := repeatedActive(t, s, fmt.Sprintf("independent-interleaving-%d.test", n))
		require.False(t, active)
	}
}

func TestRepeatedFoldingUsesRawHostDominanceWithinCanonicalWindow(t *testing.T) {
	ctx := context.Background()
	s := repeatedTestStore(t, repetition.Default())
	confirmRepeatedFixture(t, s)
	for event := 2; event <= 5; event++ {
		c, links := repeatedFixture("mirror-0.test", repeatedBody("shared"), event)
		require.NoError(t, s.AnalyzeRepeated(ctx, string(c.RequestID), c, links, 0))
	}
	independent, links := repeatedFixture("independent.mirror-0.test", repeatedBody("independent subdomain material"), 6)
	require.NoError(t, s.AnalyzeRepeated(ctx, string(independent.RequestID), independent, links, 0))
	ledger, err := report.Open(filepath.Join(t.TempDir(), "report.sqlite"), false)
	require.NoError(t, err)
	defer ledger.Close()
	for _, name := range []string{"mirror-0.test", "mirror-1.test", "mirror-2.test", "independent.mirror-0.test"} {
		uri := "http://" + name + "/"
		require.NoError(t, ledger.Discover(ctx, []report.URL{{URI: "http://outside.test/", Origin: uri}}))
		require.NoError(t, ledger.Response(ctx, uri, 200))
	}
	require.NoError(t, ledger.Response(ctx, "http://outside.test/", 200))
	require.NoError(t, s.FlushRepeated(ctx, ledger))
	path := filepath.Join(t.TempDir(), "atlas.sqlite")
	o := report.DefaultAtlasOptions()
	o.FoldRepeatedPages = true
	info, err := ledger.GenerateAtlas(ctx, path, "host-dominance", o, func(string) {})
	require.NoError(t, err)
	require.Equal(t, 3, info.Hosts, "an independent subdomain must not inherit the canonical site's repeated-host classification")
	found, err := report.AtlasSearch(ctx, path, "independent.mirror-0.test", info.ID)
	require.NoError(t, err)
	require.Len(t, found, 1)
	require.Equal(t, "independent.mirror-0.test", found[0].Name)
}

type repeatedProjectionWriter func([]repetition.Update) error

func (f repeatedProjectionWriter) RepeatedPages(u []repetition.Update) error { return f(u) }

func TestRepeatedProjectionGapsAndFailedAcknowledgment(t *testing.T) {
	ctx := context.Background()
	s := repeatedTestStore(t, repetition.Default())
	// Purging can remove arbitrary outbox entries. Progress across a long gap
	// stays bounded, and a failed destination must retain its replay entry.
	require.NoError(t, s.update(func(tx *badger.Txn) error {
		if err := write(tx, "repeated/revision", int64(300)); err != nil {
			return err
		}
		for i := 1; i < 300; i++ {
			if err := tx.Delete([]byte(fmt.Sprintf("repeated/projection/%020d", i))); err != nil {
				return err
			}
		}
		return write(tx, fmt.Sprintf("repeated/projection/%020d", 300), repetition.Update{Revision: 300})
	}))
	var seen []int64
	fail := true
	writer := repeatedProjectionWriter(func(updates []repetition.Update) error {
		if fail && updates[len(updates)-1].Revision == 300 {
			return fmt.Errorf("destination unavailable")
		}
		for _, u := range updates {
			seen = append(seen, u.Revision)
		}
		return nil
	})
	checkpoint := func() int64 {
		var value int64
		require.NoError(t, s.view(func(tx *badger.Txn) error { return repeatedRead(tx, "repeated/projected", &value) }))
		return value
	}
	require.NoError(t, s.FlushRepeated(ctx, writer))
	require.EqualValues(t, 128, checkpoint())
	require.NoError(t, s.FlushRepeated(ctx, writer))
	require.EqualValues(t, 256, checkpoint())
	err := s.FlushRepeated(ctx, writer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "destination unavailable")
	require.EqualValues(t, 256, checkpoint())
	fail = false
	require.NoError(t, s.FlushRepeated(ctx, writer))
	require.EqualValues(t, 300, checkpoint())
	require.Equal(t, []int64{128, 256, 300}, seen)
	require.NoError(t, s.FlushRepeated(ctx, writer))
	require.Equal(t, []int64{128, 256, 300}, seen)
}
