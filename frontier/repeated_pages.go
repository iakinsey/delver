package frontier

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/repetition"
	"github.com/iakinsey/delver/report"
	"github.com/iakinsey/delver/trap"
	"github.com/iakinsey/delver/types/message"
)

type repeatedBucket struct {
	Signature trap.HashVector `json:"signature"`
	Families  []string        `json:"families"`
}
type repeatedFamily struct {
	Bucket string `json:"bucket"`
	repetition.Family
	Prototype repetition.Profile `json:"prototype"`
	Evidence  []repeatedEvidence `json:"evidence"`
}
type repeatedEvidence struct {
	Site string `json:"site"`
	repeatedObservation
}
type repeatedObservation struct {
	URI       string `json:"uri"`
	Reason    string `json:"reason,omitempty"`
	Eligible  bool   `json:"eligible"`
	Event     string `json:"event"`
	Family    string `json:"family"`
	Timestamp int64  `json:"timestamp"`
	QueuedAt  int64  `json:"queued_at"`
}
type repeatedSite struct {
	repetition.Host
	Window   []repeatedObservation `json:"window"`
	LastLive int64                 `json:"last_live"`
}
type repeatedPage struct {
	ID        int64  `json:"id"`
	URI       string `json:"uri"`
	Site      string `json:"site"`
	Family    string `json:"family"`
	Event     string `json:"event"`
	Timestamp int64  `json:"timestamp"`
	QueuedAt  int64  `json:"queued_at"`
	Epoch     string `json:"epoch"`
}

func repeatedDigest(v interface{}) string {
	b, _ := json.Marshal(v)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
func repeatedPageKey(uri string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(uri))) }
func repeatedRead(tx *badger.Txn, k string, v interface{}) error {
	e := read(tx, k, v)
	if e == badger.ErrKeyNotFound {
		return nil
	}
	return e
}
func (s *Store) repeatedSettings() repetition.Settings {
	if s.params.RepeatedPages == nil {
		p := repetition.Default()
		p.Mode = "off"
		return p
	}
	return *s.params.RepeatedPages
}

// OutcomeAnalysis adds profile updates to the existing durable outcome commit.
// Ineligible pages therefore do not introduce another per-response transaction.
func (s *Store) OutcomeAnalysis(ctx context.Context, id string, c message.CompositeAnalysis, links []string) error {
	p := s.repeatedSettings()
	if p.Mode == "off" {
		return s.Outcome(c.FetcherResponse)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	profile, reason := repetition.Build(c, links, p)
	if profile.Event == "" {
		profile.Event = id
	}
	uris := []string{c.URI}
	if c.EffectiveURI != "" {
		uris = append(uris, c.EffectiveURI)
	}
	return s.policy.GuardMany(uris, func() error {
		return s.outcome(c.FetcherResponse, func(tx *badger.Txn) error { return s.observeRepeated(tx, profile, reason, true, 0) })
	})
}

// AnalyzeRepeated is also used by the resumable stored-results operation. Its
// epoch prevents old files from replacing observations made live during a job.
func (s *Store) AnalyzeRepeated(ctx context.Context, id string, c message.CompositeAnalysis, links []string, epoch int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p := s.repeatedSettings()
	if p.Mode == "off" {
		return fmt.Errorf("repeated-page analysis is off")
	}
	profile, reason := repetition.Build(c, links, p)
	if profile.Event == "" {
		profile.Event = id
	}
	uris := []string{c.URI}
	if c.EffectiveURI != "" {
		uris = append(uris, c.EffectiveURI)
	}
	return s.policy.GuardMany(uris, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.update(func(tx *badger.Txn) error { return s.observeRepeated(tx, profile, reason, epoch == 0, epoch) })
	})
}

func (s *Store) matchRepeated(tx *badger.Txn, p repetition.Profile) (string, error) {
	settings := s.repeatedSettings()
	ids, err := structureCandidates(tx, "repeated/band/", p.Structure)
	if err != nil {
		return "", err
	}
	bucketID := ""
	best := float64(-1)
	var bucket repeatedBucket
	for id := range ids {
		var b repeatedBucket
		if err := read(tx, "repeated/bucket/"+id, &b); err != nil {
			return "", err
		}
		score := trap.Similarity(b.Signature, p.Structure)
		if score >= settings.StructureSimilarity && (score > best || score == best && id < bucketID) {
			best = score
			bucketID = id
			bucket = b
		}
	}
	if bucketID == "" {
		bucketID = repeatedDigest(p.Structure)
		bucket.Signature = p.Structure
		if err := indexStructure(tx, "repeated/band/", bucketID, p.Structure); err != nil {
			return "", err
		}
	}
	familyID := ""
	best = -1.
	var family repeatedFamily
	for at := len(bucket.Families) - 1; at >= 0 && at >= len(bucket.Families)-settings.FamilyWindow; at-- {
		var f repeatedFamily
		if err := read(tx, "repeated/family/"+bucket.Families[at], &f); err == badger.ErrKeyNotFound {
			continue
		} else if err != nil {
			return "", err
		}
		m := repetition.Compare(p, f.Prototype)
		score := m.Structure + m.Text + m.Destinations
		if m.Qualifies(settings) && (score > best || score == best && f.ID < familyID) {
			familyID = f.ID
			family = f
			family.Measurements = m
			best = score
		}
	}
	if familyID == "" {
		familyID = repeatedDigest(struct{ Structure, Text trap.HashVector }{p.Structure, p.Text})
		if err := repeatedRead(tx, "repeated/family/"+familyID, &family); err != nil {
			return "", err
		}
		family.ID = familyID
		family.Representative = p.URI
		family.Prototype = p
		family.Measurements = repetition.Measurement{Structure: 1, Text: 1, Destinations: 1}
	}
	family.Bucket = bucketID
	was := family.Epoch == s.repeatedEpoch && family.Confirmed && !family.Exempt
	if family.Epoch != s.repeatedEpoch {
		family.Evidence = nil
		family.Epoch = s.repeatedEpoch
	}
	family.Evidence = append(family.Evidence, repeatedEvidence{p.Site, repeatedObservation{URI: p.URI, Event: p.Event, Family: familyID, Timestamp: p.Timestamp, QueuedAt: p.QueuedAt, Eligible: true}})
	retainRepeatedEvidence(&family, settings)
	if was != (family.Confirmed && !family.Exempt) {
		if err := s.markRepeatedRouting(tx); err != nil {
			return "", err
		}
	}
	if err := write(tx, "repeated/family/"+familyID, family); err != nil {
		return "", err
	}
	kept := bucket.Families[:0]
	for _, id := range bucket.Families {
		if id != familyID {
			kept = append(kept, id)
		}
	}
	bucket.Families = append(kept, familyID)
	if len(bucket.Families) > settings.FamilyWindow {
		bucket.Families = bucket.Families[len(bucket.Families)-settings.FamilyWindow:]
	}
	return familyID, write(tx, "repeated/bucket/"+bucketID, bucket)
}

func (s *Store) observeRepeated(tx *badger.Txn, p repetition.Profile, reason string, live bool, epoch int64) error {
	settings := s.repeatedSettings()
	key := "repeated/page/" + repeatedPageKey(p.URI)
	var page repeatedPage
	if err := repeatedRead(tx, key, &page); err != nil {
		return err
	}
	if page.Epoch == s.repeatedEpoch && page.Event != "" && page.Event == p.Event {
		return nil
	}
	if page.Timestamp > p.Timestamp || page.Timestamp == p.Timestamp && page.QueuedAt > p.QueuedAt {
		return nil
	}
	var host repeatedSite
	if err := repeatedRead(tx, "repeated/site/"+p.Site, &host); err != nil {
		return err
	}
	if !live && epoch > 0 && host.LastLive >= epoch {
		return nil
	}
	for _, o := range host.Window {
		if host.Epoch == s.repeatedEpoch && o.Event == p.Event {
			return nil
		}
	}
	// Old replay outside the retained window cannot displace newer evidence.
	if host.Epoch == s.repeatedEpoch && len(host.Window) >= settings.SiteWindow {
		old := host.Window[0]
		if p.Timestamp < old.Timestamp || p.Timestamp == old.Timestamp && p.QueuedAt < old.QueuedAt {
			return nil
		}
	}
	field := "eligible"
	if reason != "" {
		field = reason
	}
	if err := increment(tx, "repeated/coverage/"+field, 1); err != nil {
		return err
	}
	familyID := ""
	var err error
	if reason == "" {
		familyID, err = s.matchRepeated(tx, p)
		if err != nil {
			return err
		}
	}
	counts := func(window []repeatedObservation) map[string]int {
		v := map[string]int{}
		for _, o := range window {
			if o.Family != "" {
				v[o.Family]++
			}
		}
		return v
	}
	oldCounts := counts(host.Window)
	if host.Epoch != s.repeatedEpoch {
		host.Window = nil
	}
	oldWindow := append([]repeatedObservation{}, host.Window...)
	oldActive, err := s.repeatedFamilyActive(tx, page.Family)
	if err != nil {
		return err
	}
	host.Window = append(host.Window, repeatedObservation{URI: p.URI, Reason: reason, Event: p.Event, Family: familyID, Timestamp: p.Timestamp, QueuedAt: p.QueuedAt, Eligible: reason == ""})
	sort.SliceStable(host.Window, func(i, j int) bool {
		a, b := host.Window[i], host.Window[j]
		if a.Timestamp != b.Timestamp {
			return a.Timestamp < b.Timestamp
		}
		if a.QueuedAt != b.QueuedAt {
			return a.QueuedAt < b.QueuedAt
		}
		return a.Event < b.Event
	})
	if len(host.Window) > settings.SiteWindow {
		host.Window = host.Window[len(host.Window)-settings.SiteWindow:]
	}
	newCounts := counts(host.Window)
	changed := map[string]bool{}
	for id := range oldCounts {
		changed[id] = true
	}
	for id := range newCounts {
		changed[id] = true
	}
	delta := repetition.Update{}
	priorityChanged := false
	for id := range changed {
		var f repeatedFamily
		if err := read(tx, "repeated/family/"+id, &f); err == badger.ErrKeyNotFound {
			continue
		} else if err != nil {
			return err
		}
		was := f.Epoch == s.repeatedEpoch && f.Confirmed && !f.Exempt
		if oldCounts[id] == 0 && newCounts[id] > 0 {
			f.Sites++
		}
		if oldCounts[id] > 0 && newCounts[id] == 0 {
			f.Sites--
		}
		allowed := map[string]bool{}
		for _, o := range host.Window {
			if o.Family == id {
				allowed[o.Event] = true
			}
		}
		kept := f.Evidence[:0]
		for _, e := range f.Evidence {
			if e.Site != p.Site || allowed[e.Event] {
				kept = append(kept, e)
			}
		}
		f.Evidence = kept
		retainRepeatedEvidence(&f, settings)
		priorityChanged = priorityChanged || (was != (f.Confirmed && !f.Exempt))
		delta.Families = append(delta.Families, f.Family)
		if newCounts[id] > 0 {
			if err := write(tx, "repeated/member/"+id+"/"+p.Site, newCounts[id]); err != nil {
				return err
			}
		} else if err := tx.Delete([]byte("repeated/member/" + id + "/" + p.Site)); err != nil {
			return err
		}
		if f.Sites == 0 && !f.Exempt {
			if err := s.removeRepeatedPrototype(tx, f); err != nil {
				return err
			}
		} else if err := write(tx, "repeated/family/"+id, f); err != nil {
			return err
		}
	}
	host.Site = p.Site
	host.URI = p.URI
	host.Epoch = s.repeatedEpoch
	host.Samples = len(host.Window)
	host.Eligible = 0
	host.Reason = reason
	for _, o := range host.Window {
		if o.Eligible {
			host.Eligible++
		}
	}
	host.Family = ""
	host.Dominance = 0
	for id, n := range newCounts {
		ratio := float64(n) / float64(max(1, host.Eligible))
		if ratio >= settings.HostDominance && (ratio > host.Dominance || ratio == host.Dominance && id < host.Family) {
			host.Family = id
			host.Dominance = ratio
		}
	}
	if live {
		host.LastLive = time.Now().UnixNano()
	}
	if err := write(tx, "repeated/site/"+p.Site, host); err != nil {
		return err
	}
	if page.ID == 0 {
		if err := increment(tx, "repeated/source_sequence", 1); err != nil {
			return err
		}
		if err := read(tx, "repeated/source_sequence", &page.ID); err != nil {
			return err
		}
	}
	current := repeatedPage{ID: page.ID, URI: p.URI, Site: p.Site, Family: familyID, Event: p.Event, Timestamp: p.Timestamp, QueuedAt: p.QueuedAt, Epoch: s.repeatedEpoch}
	if err := write(tx, key, current); err != nil {
		return err
	}
	if err := write(tx, fmt.Sprintf("repeated/source/%020d", page.ID), current); err != nil {
		return err
	}
	active, err := s.repeatedFamilyActive(tx, familyID)
	if err != nil {
		return err
	}
	affected := map[string]string{repeatedHostname(p.URI): p.URI}
	retained := map[string]bool{}
	for _, o := range host.Window {
		retained[o.Event] = true
	}
	for _, o := range oldWindow {
		if !retained[o.Event] {
			affected[repeatedHostname(o.URI)] = o.URI
		}
	}
	membershipChanged := false
	for name, uri := range affected {
		if name == "" {
			continue
		}
		before := repeatedHostMembership(oldWindow, uri, p.Site, s.repeatedEpoch, settings)
		after := repeatedHostMembership(host.Window, uri, p.Site, s.repeatedEpoch, settings)
		was, err := s.repeatedFamilyActive(tx, before.Family)
		if err != nil {
			return err
		}
		now, err := s.repeatedFamilyActive(tx, after.Family)
		if err != nil {
			return err
		}
		membershipChanged = membershipChanged || before.Family != after.Family && (was || now)
		delta.Hosts = append(delta.Hosts, after)
	}
	if membershipChanged || priorityChanged || page.Event != "" && (oldActive != active || page.Family != familyID && (oldActive || active)) || !live && active {
		if err := s.markRepeatedRouting(tx); err != nil {
			return err
		}
	}

	return s.enqueueRepeatedProjection(tx, delta)
}

func (s *Store) repeatedFamilyActive(tx *badger.Txn, id string) (bool, error) {
	if id == "" {
		return false, nil
	}
	var f repeatedFamily
	if err := read(tx, "repeated/family/"+id, &f); err == badger.ErrKeyNotFound {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return f.Epoch == s.repeatedEpoch && f.SupportSites >= s.repeatedSettings().MinimumSites && !f.Exempt, nil
}

// A classifier lives for one bounded database transaction. Repeated provenance
// checks share decoded evidence; no raw bodies or cross-delivery caches persist.
type repeatedClassifier struct {
	store      *Store
	tx         *badger.Txn
	sources    map[int64]bool
	references map[string]int64
	hosts      map[string]repeatedSite
	families   map[string]bool
	rawHosts   map[string]repetition.Host
}

func newRepeatedClassifier(s *Store, tx *badger.Txn) *repeatedClassifier {
	return &repeatedClassifier{store: s, tx: tx, sources: map[int64]bool{}, references: map[string]int64{}, hosts: map[string]repeatedSite{}, families: map[string]bool{}, rawHosts: map[string]repetition.Host{}}
}

func (c *repeatedClassifier) reference(uri string) (int64, error) {
	if id, known := c.references[uri]; known {
		return id, nil
	}
	var p repeatedPage
	if err := repeatedRead(c.tx, "repeated/page/"+repeatedPageKey(uri), &p); err != nil {
		return 0, err
	}
	c.references[uri] = p.ID
	return p.ID, nil
}
func (c *repeatedClassifier) source(id int64) (bool, error) {
	if id == 0 {
		return false, nil
	}
	if value, ok := c.sources[id]; ok {
		return value, nil
	}
	var p repeatedPage
	if err := repeatedRead(c.tx, fmt.Sprintf("repeated/source/%020d", id), &p); err != nil {
		return false, err
	}
	if p.Epoch != c.store.repeatedEpoch {
		c.sources[id] = false
		return false, nil
	}
	h, known := c.hosts[p.Site]
	if !known {
		if err := repeatedRead(c.tx, "repeated/site/"+p.Site, &h); err != nil {
			return false, err
		}
		c.hosts[p.Site] = h
	}
	name := repeatedHostname(p.URI)
	raw, known := c.rawHosts[name]
	if !known {
		raw = repeatedHostMembership(h.Window, p.URI, p.Site, h.Epoch, c.store.repeatedSettings())
		c.rawHosts[name] = raw
	}
	if raw.Epoch != c.store.repeatedEpoch || raw.Family != p.Family {
		c.sources[id] = false
		return false, nil
	}
	active, known := c.families[p.Family]
	if !known {
		var err error
		active, err = c.store.repeatedFamilyActive(c.tx, p.Family)
		if err != nil {
			return false, err
		}
		c.families[p.Family] = active
	}
	c.sources[id] = active
	return active, nil
}
func (s *Store) candidateRepeated(tx *badger.Txn, c Candidate, caches ...*repeatedClassifier) (bool, error) {
	if s.repeatedSettings().Mode != "deprioritize" || c.NormalPinned {
		return false, nil
	}
	var cache *repeatedClassifier
	if len(caches) > 0 {
		cache = caches[0]
	} else {
		cache = newRepeatedClassifier(s, tx)
	}
	origins := c.Origins
	if len(origins) == 0 {
		if c.Request.Origin == "" {
			return false, nil
		}
		id, err := cache.reference(c.Request.Origin)
		if err != nil {
			return false, err
		}
		origins = []int64{id}
	}
	for _, id := range origins {
		ok, err := cache.source(id)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}
func (s *Store) mergeRepeatedOrigins(c *Candidate, origin string, cache *repeatedClassifier) error {
	if origin == "" {
		c.NormalPinned = true
		return nil
	}
	if origin == c.Request.Origin || c.NormalPinned {
		return nil
	}
	if len(c.Origins) == 0 {
		if c.Request.Origin == "" {
			c.NormalPinned = true
			return nil
		}
		id, err := cache.reference(c.Request.Origin)
		if err != nil {
			return err
		}
		if id == 0 {
			c.NormalPinned = true
			return nil
		}
		c.Origins = append(c.Origins, id)
	}
	id, err := cache.reference(origin)
	if err != nil {
		return err
	}
	if id == 0 {
		c.NormalPinned = true
		return nil
	}
	for _, old := range c.Origins {
		if id == old {
			return nil
		}
	}
	if len(c.Origins) >= 128 {
		c.NormalPinned = true
		c.Origins = nil
		return nil
	}
	c.Origins = append(c.Origins, id)
	return nil
}

func (s *Store) enqueueRepeatedProjection(tx *badger.Txn, delta repetition.Update) error {
	var revision int64
	if err := repeatedRead(tx, "repeated/revision", &revision); err != nil {
		return err
	}
	revision++
	delta.Revision = revision
	delta.Epoch = s.repeatedEpoch
	if s.repeatedSettings().Mode == "off" {
		delta.Epoch = ""
	}
	if err := write(tx, "repeated/revision", revision); err != nil {
		return err
	}
	return write(tx, fmt.Sprintf("repeated/projection/%020d", revision), delta)
}
func (s *Store) FlushRepeated(ctx context.Context, w repetition.Writer) error {
	if w == nil {
		return nil
	}
	updates := []repetition.Update{}
	keys := []string{}
	var through int64
	epoch := s.repeatedEpoch
	if s.repeatedSettings().Mode == "off" {
		epoch = ""
	}
	if err := s.view(func(tx *badger.Txn) error {
		var revision, projected int64
		if err := repeatedRead(tx, "repeated/revision", &revision); err != nil {
			return err
		}
		if err := repeatedRead(tx, "repeated/projected", &projected); err != nil {
			return err
		}
		if projected >= revision {
			return nil
		}
		// Sequence keys avoid repeatedly seeking through deleted outbox entries.
		// Protection may remove entries; gaps still consume bounded progress.
		through = min(revision, projected+128)
		for next := projected + 1; next <= through; next++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			k := fmt.Sprintf("repeated/projection/%020d", next)
			var u repetition.Update
			if err := read(tx, k, &u); err == badger.ErrKeyNotFound {
				continue
			} else if err != nil {
				return err
			}
			updates = append(updates, u)
			keys = append(keys, k)
		}
		return nil
	}); err != nil {
		return err
	}
	if through > 0 && (len(updates) == 0 || updates[len(updates)-1].Revision < through) {
		// Keep report-side completion barriers in step even when protection
		// removed the final records in this range. This carries no page data.
		updates = append(updates, repetition.Update{Revision: through, Epoch: epoch})
	}
	if len(updates) > 0 {
		if err := w.RepeatedPages(updates); err != nil {
			return err
		}
	}
	if through == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.update(func(tx *badger.Txn) error {
		for _, k := range keys {
			if err := tx.Delete([]byte(k)); err != nil {
				return err
			}
		}
		var projected int64
		if err := repeatedRead(tx, "repeated/projected", &projected); err != nil {
			return err
		}
		if through > projected {
			return write(tx, "repeated/projected", through)
		}
		return nil
	})
}

type RepeatedView struct {
	Mode           string              `json:"mode"`
	DowngradeReady bool                `json:"downgrade_ready"`
	Revision       int64               `json:"revision"`
	Families       []repetition.Family `json:"families"`
	Hosts          []repetition.Host   `json:"hosts"`
	Coverage       map[string]int64    `json:"coverage"`
	After          string              `json:"after,omitempty"`
	Routing        RepeatedRouting     `json:"routing"`
}

func (s *Store) RepeatedPages(ctx context.Context, query, after string, limit int) (RepeatedView, error) {
	if len(query) > 253 || len(after) > 260 {
		return RepeatedView{}, fmt.Errorf("family query or cursor too long")
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	v := RepeatedView{Mode: s.repeatedSettings().Mode, Families: []repetition.Family{}, Hosts: []repetition.Host{}, Coverage: map[string]int64{}}
	query = strings.ToLower(strings.TrimSpace(query))
	err := s.view(func(tx *badger.Txn) error {
		if err := repeatedRead(tx, "repeated/revision", &v.Revision); err != nil {
			return err
		}
		if err := repeatedRead(tx, "repeated/routing", &v.Routing); err != nil {
			return err
		}
		v.DowngradeReady = v.Mode == "off" && v.Routing.Phase == ""
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := tx.NewIterator(opts)
		defer it.Close()
		for it.Seek([]byte("repeated/coverage/")); it.ValidForPrefix([]byte("repeated/coverage/")); it.Next() {
			var n int64
			k := string(it.Item().Key())
			if err := read(tx, k, &n); err != nil {
				return err
			}
			v.Coverage[strings.TrimPrefix(k, "repeated/coverage/")] = n
		}
		if query != "" && !(len(query) >= 8 && strings.Trim(query, "0123456789abcdef") == "") {
			prefix := "repeated/site/"
			start := prefix
			if strings.HasPrefix(after, "site:") {
				start += strings.TrimPrefix(after, "site:") + "\x00"
			}
			seen := map[string]bool{}
			inspected := 0
			for it.Seek([]byte(start)); it.ValidForPrefix([]byte(prefix)) && len(v.Families) < limit && inspected < 128; it.Next() {
				if err := ctx.Err(); err != nil {
					return err
				}
				inspected++
				key := string(it.Item().Key())
				name := strings.TrimPrefix(key, prefix)
				v.After = "site:" + name
				if !strings.Contains(name, query) {
					continue
				}
				var h repeatedSite
				if err := read(tx, key, &h); err != nil {
					return err
				}
				for _, o := range h.Window {
					if o.Family == "" || seen[o.Family] {
						continue
					}
					seen[o.Family] = true
					var f repeatedFamily
					if err := read(tx, "repeated/family/"+o.Family, &f); err == badger.ErrKeyNotFound {
						continue
					} else if err != nil {
						return err
					}
					f.Confirmed = f.Epoch == s.repeatedEpoch && f.SupportSites >= s.repeatedSettings().MinimumSites
					v.Families = append(v.Families, f.Family)
					if len(v.Families) >= limit {
						break
					}
				}
			}
			if !it.ValidForPrefix([]byte(prefix)) {
				v.After = ""
			}
			return nil
		}
		start := "repeated/family/"
		if after != "" {
			start += after + "\x00"
		}
		inspected := 0
		for it.Seek([]byte(start)); it.ValidForPrefix([]byte("repeated/family/")) && len(v.Families) < limit && inspected < 128; it.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}
			inspected++
			k := string(it.Item().Key())
			var f repeatedFamily
			if err := read(tx, k, &f); err != nil {
				return err
			}
			v.After = f.ID
			if query == "" || strings.Contains(f.ID, query) || strings.Contains(f.Prototype.Site, query) {
				f.Confirmed = f.Epoch == s.repeatedEpoch && f.SupportSites >= s.repeatedSettings().MinimumSites
				v.Families = append(v.Families, f.Family)
			}
		}
		if !it.ValidForPrefix([]byte("repeated/family/")) {
			v.After = ""
		}
		return nil
	})
	return v, err
}
func (s *Store) SetFamilyExempt(ctx context.Context, id string, exempt bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.update(func(tx *badger.Txn) error {
		var f repeatedFamily
		if err := read(tx, "repeated/family/"+id, &f); err != nil {
			return err
		}
		if f.Exempt == exempt {
			return nil
		}
		f.Exempt = exempt
		f.Confirmed = f.Epoch == s.repeatedEpoch && f.SupportSites >= s.repeatedSettings().MinimumSites
		if err := write(tx, "repeated/family/"+id, f); err != nil {
			return err
		}
		if err := s.markRepeatedRouting(tx); err != nil {
			return err
		}
		return s.enqueueRepeatedProjection(tx, repetition.Update{Families: []repetition.Family{f.Family}})
	})
}

func retainRepeatedEvidence(f *repeatedFamily, p repetition.Settings) {
	sort.SliceStable(f.Evidence, func(i, j int) bool {
		a, b := f.Evidence[i], f.Evidence[j]
		if a.Timestamp != b.Timestamp {
			return a.Timestamp < b.Timestamp
		}
		if a.QueuedAt != b.QueuedAt {
			return a.QueuedAt < b.QueuedAt
		}
		return a.Event < b.Event
	})
	perSite := map[string]int{}
	kept := make([]repeatedEvidence, 0, min(len(f.Evidence), p.FamilyWindow))
	for i := len(f.Evidence) - 1; i >= 0 && len(kept) < p.FamilyWindow; i-- {
		e := f.Evidence[i]
		if perSite[e.Site] >= p.SiteWindow {
			continue
		}
		perSite[e.Site]++
		kept = append(kept, e)
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	f.Evidence = kept
	f.Observations = len(kept)
	f.SupportSites = len(perSite)
	f.Confirmed = f.SupportSites >= p.MinimumSites
}

type RepeatedMembers struct {
	Hosts []repetition.Host `json:"hosts"`
	After string            `json:"after,omitempty"`
}

func (s *Store) RepeatedMembers(ctx context.Context, family, query, after string, limit int) (out RepeatedMembers, err error) {
	if len(family) != 64 || len(query) > 253 || len(after) > 253 {
		return out, fmt.Errorf("invalid family member query")
	}
	limit = min(100, max(1, limit))
	out.Hosts = []repetition.Host{}
	err = s.view(func(tx *badger.Txn) error {
		prefix := "repeated/member/" + family + "/"
		start := prefix
		if after != "" {
			start += after + "\x00"
		}
		it := tx.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		inspected := 0
		for it.Seek([]byte(start)); it.ValidForPrefix([]byte(prefix)) && len(out.Hosts) < limit && inspected < 128; it.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}
			inspected++
			key := strings.TrimPrefix(string(it.Item().Key()), prefix)
			out.After = key
			if !strings.Contains(key, strings.ToLower(query)) {
				continue
			}
			var h repeatedSite
			if err := repeatedRead(tx, "repeated/site/"+key, &h); err != nil {
				return err
			}
			var count int
			if err := read(tx, string(it.Item().Key()), &count); err != nil {
				return err
			}
			h.FamilyObservations = count
			out.Hosts = append(out.Hosts, h.Host)
		}
		if !it.ValidForPrefix([]byte(prefix)) {
			out.After = ""
		}
		return nil
	})
	return
}

func (s *Store) removeRepeatedPrototype(tx *badger.Txn, f repeatedFamily) error {
	if err := tx.Delete([]byte("repeated/family/" + f.ID)); err != nil {
		return err
	}
	if f.Bucket == "" {
		return nil
	}
	var bucket repeatedBucket
	if err := repeatedRead(tx, "repeated/bucket/"+f.Bucket, &bucket); err != nil {
		return err
	}
	ids := bucket.Families[:0]
	for _, id := range bucket.Families {
		if id != f.ID {
			ids = append(ids, id)
		}
	}
	bucket.Families = ids
	if len(ids) > 0 {
		return write(tx, "repeated/bucket/"+f.Bucket, bucket)
	}
	for band := 0; band < 16 && len(bucket.Signature) == 64; band++ {
		key := fmt.Sprintf("repeated/band/%02d/%016x%016x%016x%016x/%s", band, bucket.Signature[band*4], bucket.Signature[band*4+1], bucket.Signature[band*4+2], bucket.Signature[band*4+3], f.Bucket)
		if err := tx.Delete([]byte(key)); err != nil {
			return err
		}
	}
	return tx.Delete([]byte("repeated/bucket/" + f.Bucket))
}

// Host dominance is computed from the retained canonical-site window. A sibling
// host cannot inherit another host's observations, and subdomains still provide
// only one canonical site's confirmation evidence.
func repeatedHostname(uri string) string {
	_, name, err := report.Normalize(uri)
	if err != nil {
		return ""
	}
	return name
}
func repeatedHostMembership(window []repeatedObservation, uri, canonical, epoch string, settings repetition.Settings) repetition.Host {
	name := repeatedHostname(uri)
	h := repetition.Host{Host: name, Site: canonical, URI: uri, Epoch: epoch}
	counts := map[string]int{}
	for _, o := range window {
		if repeatedHostname(o.URI) != name {
			continue
		}
		h.Samples++
		h.URI = o.URI
		h.Reason = o.Reason
		if o.Eligible {
			h.Eligible++
			if o.Family != "" {
				counts[o.Family]++
			}
		}
	}
	for id, n := range counts {
		ratio := float64(n) / float64(max(1, h.Eligible))
		if ratio >= settings.HostDominance && (ratio > h.Dominance || ratio == h.Dominance && id < h.Family) {
			h.Family = id
			h.Dominance = ratio
		}
	}
	return h
}
