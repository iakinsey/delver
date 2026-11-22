package protection

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func policy(t *testing.T) *Policy {
	t.Helper()
	p, e := Open(filepath.Join(t.TempDir(), "policy.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Close() })
	return p
}
func TestListParsingScopeAndErrors(t *testing.T) {
	es, e := ParseList("# test\nEXAMPLE.test\nhttps://other.test/a#x\nexample.test\n")
	if e != nil || len(es) != 2 {
		t.Fatal(es, e)
	}
	if !es[0].Matches("http://a.example.test/") || es[0].Matches("http://notexample.test/") || !es[1].Matches("https://other.test/a") || es[1].Matches("https://other.test/b") {
		t.Fatal(es)
	}
	_, e = ParseList("example.test\n*.invalid.test")
	if e == nil || !strings.Contains(e.Error(), "line 2") {
		t.Fatal(e)
	}
}
func TestPolicyListsCancellationOverlapAndRevisions(t *testing.T) {
	p := policy(t)
	ctx, done, e := p.Begin(context.Background(), "http://a.example.test/")
	if e != nil {
		t.Fatal(e)
	}
	defer done()
	l := List{ID: "one", Name: "One", Enabled: true, Refresh: "6h", Updated: time.Now(), Entries: []Entry{{Value: "example.test", Domain: true}}}
	if e = p.Update(1, []List{l}, nil); e != nil {
		t.Fatal(e)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("active request not cancelled")
	}
	if !errors.Is(p.Check("http://a.example.test/"), ErrBlocked) {
		t.Fatal("not blocked")
	}
	if e = p.Update(1, nil, nil); e == nil {
		t.Fatal("stale revision accepted")
	}
	l2 := l
	l2.ID = "two"
	if e = p.Update(2, []List{l, l2}, nil); e != nil {
		t.Fatal(e)
	}
	l.Enabled = false
	if e = p.Update(3, []List{l, l2}, nil); e != nil {
		t.Fatal(e)
	}
	if len(p.Match("http://example.test/")) != 1 {
		t.Fatal("disabled source still active")
	}
}
func TestIncidentPersistenceNoPayloadAndIndependentBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.sqlite")
	p, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	r := []Rule{{ID: "fixture", Enabled: true, Phrases: []string{"synthetic restricted marker"}}}
	if e = p.Update(1, nil, r); e != nil {
		t.Fatal(e)
	}
	if e = p.Inspect("http://a.service.onion/page", []byte("<p>synthetic restricted marker</p>"), "crawl", "run", 2); !errors.Is(e, ErrBlocked) {
		t.Fatal(e)
	}
	s := p.Snapshot()
	if len(s.Incidents) != 1 || s.Incidents[0].Targets[0].Site != "service.onion" || s.Incidents[0].Depth != 2 {
		t.Fatal(s)
	}
	p.Close()
	p, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if !errors.Is(p.Check("http://b.service.onion/"), ErrBlocked) {
		t.Fatal("lost block")
	}
	if e = p.Update(p.Snapshot().Revision, nil, nil); e != nil {
		t.Fatal(e)
	}
	if !errors.Is(p.Check("http://service.onion/"), ErrBlocked) {
		t.Fatal("rule removal removed quarantine")
	}
}
func TestAmbiguousTermsDoNotTrigger(t *testing.T) {
	p := policy(t)
	for _, body := range []string{"CPAN package archive", "CP command documentation", "report child sexual abuse material"} {
		if e := p.Inspect("http://example.test/", []byte(body), "", "", 0); e != nil {
			t.Fatal(e)
		}
	}
}

func TestDisablingListCancelsItsPurgeWithoutOverridingOtherSources(t *testing.T) {
	p := policy(t)
	entries, e := ParseList("only.test\noverlap.test")
	if e != nil {
		t.Fatal(e)
	}
	first := List{ID: "first", Name: "first", Enabled: true, Refresh: "6h", Updated: time.Now(), Entries: entries, Version: entryVersion(entries)}
	if e = p.Activate(1, []List{first}, nil, first); e != nil {
		t.Fatal(e)
	}
	pending := p.Pending()
	if len(pending) != 1 {
		t.Fatal(pending)
	}
	ctx, finish, e := p.PurgeContext(context.Background(), pending[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	defer finish()
	second := List{ID: "second", Name: "second", Enabled: true, Refresh: "6h", Updated: time.Now(), Entries: []Entry{{Domain: true, Value: "overlap.test"}}}
	first.Enabled = false
	if e = p.Update(2, []List{first, second}, nil); e != nil {
		t.Fatal(e)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("disabled purge continued")
	}
	if e = p.SetIncident(pending[0]); !errors.Is(e, ErrCancelled) {
		t.Fatal("cancelled operation resurrected", e)
	}
	if p.Check("http://only.test/") != nil || !errors.Is(p.Check("http://overlap.test/"), ErrBlocked) {
		t.Fatal("source-specific disable failed")
	}
	if p.HasUnfinished() {
		t.Fatal("cancelled job kept snapshots unavailable")
	}
}

func TestAuthenticatedDestinationsAreMatchedAndIncidentLocationOmitsCredentials(t *testing.T) {
	p := policy(t)
	entries, e := ParseList("blocked.test")
	if e != nil {
		t.Fatal(e)
	}
	match := NewMatcher(entries, nil).Matches
	if !match("http://operator:synthetic-password@blocked.test/path") {
		t.Fatal("authenticated destination escaped purge")
	}
	if e = p.Check("http://operator:synthetic-password@allowed.test/path"); e != nil {
		t.Fatal("authenticated destination rejected", e)
	}
	incident, e := p.Trigger("http://operator:synthetic-password@blocked.test/path", "synthetic", "crawl", "run", 0)
	if !errors.Is(e, ErrBlocked) || strings.Contains(incident.URL, "synthetic-password") || strings.Contains(incident.URL, "operator") {
		t.Fatal(incident, e)
	}
	if !errors.Is(p.Check("http://another:secret@blocked.test/path"), ErrBlocked) {
		t.Fatal("HTTP credentials bypassed quarantine")
	}
}

func TestPublicReportsAreBoundedAndPagedWithoutPrivateFeedEntries(t *testing.T) {
	p := policy(t)
	incident, e := p.Trigger("http://root.test/", "synthetic", "crawl", "run", 3)
	if !errors.Is(e, ErrBlocked) {
		t.Fatal(e)
	}
	for n := 1; n <= 1000; n++ {
		incident.Targets = append(incident.Targets, Target{Site: fmt.Sprintf("s-%04d.test", n), Distance: 1})
	}
	if e = p.SetIncident(incident); e != nil {
		t.Fatal(e)
	}
	public := p.PublicSnapshot()
	if len(public.Incidents) != 1 || public.Incidents[0].TargetCount != 1001 || len(public.Incidents[0].Targets) != 0 {
		t.Fatal("poll response contains unbounded targets")
	}
	page, e := p.Incident(incident.ID, 200, 200)
	if e != nil || len(page.Targets) != 200 || page.TargetCount != 1001 || page.Targets[0].Site != "s-0200.test" {
		t.Fatal(page, e)
	}
	all, e := p.Incident(incident.ID, 0, 2000)
	if e != nil || len(all.Targets) != 1001 {
		t.Fatal("metadata export truncated", e)
	}
}

func TestIPv6SiteQuarantineAndPurgeMatching(t *testing.T) {
	p := policy(t)
	i, e := p.Trigger("http://[2001:db8::1]:8080/path", "synthetic", "crawl", "run", 0)
	if !errors.Is(e, ErrBlocked) {
		t.Fatal(e)
	}
	if !errors.Is(p.Check("https://[2001:db8::1]/other"), ErrBlocked) || p.Check("http://[2001:db8::2]/") != nil {
		t.Fatal("IP site identity changed")
	}
	matcher := NewMatcher(nil, i.Targets)
	raw := []byte(`{"uri":"http://[2001:db8::2]/","origin":"http://[2001:db8::1]:8080/path"}`)
	clean, changed, e := SanitizeJSON(raw, matcher.Matches)
	if e != nil || !changed || strings.Contains(string(clean), "2001:db8::1") {
		t.Fatal(string(clean), changed, e)
	}
}

func TestRulePunctuationCannotTurnAmbiguousAcronymsIntoContext(t *testing.T) {
	p := policy(t)
	for _, phrase := range []string{"!!!!CPAN!!!!", "*** CP ***", "........"} {
		e := p.Update(1, nil, []Rule{{ID: "synthetic", Enabled: true, Phrases: []string{phrase}}})
		if e == nil {
			t.Fatalf("ambiguous rule accepted: %s", phrase)
		}
	}
	if phraseMatches("prefixsynthetic restricted marker", "synthetic restricted marker") || phraseMatches("synthetic restricted markersuffix", "synthetic restricted marker") {
		t.Fatal("partial-word phrase triggered")
	}
	if !phraseMatches("A synthetic restricted marker.", "synthetic restricted marker") {
		t.Fatal("bounded phrase missed")
	}
}
