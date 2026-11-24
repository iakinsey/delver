package frontier

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/site"
	"github.com/iakinsey/delver/trap"
	"github.com/iakinsey/delver/types/message"
	"strings"
	"time"
)

type TrapMeasurement struct {
	CrossSiteLinks float64 `json:"cross_site_links"`
	FreshLinks     float64 `json:"fresh_links"`
	Site           string  `json:"site"`
	URI            string  `json:"uri"`
	Redundancy     float64 `json:"redundancy"`
	Novelty        float64 `json:"novelty"`
	Links          int     `json:"links"`
}
type TrapFamily struct {
	SitePatterns map[string][]uint64 `json:"site_patterns"`
	URLs         []uint64            `json:"urls"`
	ID           string              `json:"id"`
	Signature    []uint64            `json:"signature"`
	Observations []TrapMeasurement   `json:"observations"`
	Patterns     []uint64            `json:"patterns"`
	Destinations []string            `json:"destinations"`
}
type TrapDecision struct {
	Gateway        *GatewayCluster `json:"gateway,omitempty"`
	CrossSiteLinks float64         `json:"cross_site_links"`
	FreshLinks     float64         `json:"fresh_links"`
	Site           string          `json:"site"`
	Family         string          `json:"family"`
	At             time.Time       `json:"at"`
	Samples        int             `json:"samples"`
	Sites          int             `json:"sites"`
	Similarity     float64         `json:"similarity"`
	Redundancy     float64         `json:"redundancy"`
	Novelty        float64         `json:"novelty"`
	Reason         string          `json:"reason"`
	Settings       trap.Settings   `json:"settings"`
}

func (s *Store) ObserveTrap(ctx context.Context, o trap.Observation) (bool, error) {
	p := s.params.Traps
	if p == nil || !p.Enabled || len(o.Signature) != 64 || o.Tokens < 5 {
		return false, nil
	}
	if e := ctx.Err(); e != nil {
		return false, e
	}
	blocked := false
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.update(func(tx *badger.Txn) error {
		if _, e := tx.Get([]byte("trap/allow/" + o.Site)); e == nil {
			return nil
		}
		if _, e := tx.Get([]byte("trap/blocked/" + o.Site)); e == nil {
			blocked = true
			return nil
		} else if e != badger.ErrKeyNotFound {
			return e
		}
		candidates, err := structureCandidates(tx, "trap/band/", o.Signature)
		if err != nil {
			return err
		}
		family := TrapFamily{}
		similarity := float64(0)
		for id := range candidates {
			var f TrapFamily
			if e := read(tx, "trap/family/"+id, &f); e != nil {
				return e
			}
			score := trap.Similarity(o.Signature, f.Signature)
			if score >= p.Similarity && (score > similarity || score == similarity && id < family.ID) {
				family = f
				similarity = score
			}
		}
		if family.ID == "" {
			// Sparse pages cannot seed a link-network suspicion. Once a family is
			// under observation, sparse pages still reset its consecutive evidence.
			if o.Links < p.MinimumLinks {
				return nil
			}
			raw, _ := json.Marshal(o.Signature)
			digest := sha256.Sum256(raw)
			family = TrapFamily{ID: fmt.Sprintf("%x", digest[:12]), Signature: o.Signature}
			similarity = 1
			for band := 0; band < 16; band++ {
				key := fmt.Sprintf("trap/band/%02d/%016x%016x%016x%016x/%s", band, o.Signature[band*4], o.Signature[band*4+1], o.Signature[band*4+2], o.Signature[band*4+3], family.ID)
				if e := write(tx, key, true); e != nil {
					return e
				}
			}
		}
		patterns := map[uint64]bool{}
		for _, v := range family.Patterns {
			patterns[v] = true
		}
		destinations := map[string]bool{}
		for _, v := range family.Destinations {
			destinations[v] = true
		}
		if family.SitePatterns == nil {
			family.SitePatterns = map[string][]uint64{}
		}
		localPatterns := map[uint64]bool{}
		for _, v := range family.SitePatterns[o.Site] {
			localPatterns[v] = true
		}
		repeat, newSites, newPatterns := 0, 0, 0
		for _, v := range o.Patterns {
			if patterns[v] {
				repeat++
			}
			if !localPatterns[v] {
				newPatterns++
			}
			if !localPatterns[v] {
				family.SitePatterns[o.Site] = append(family.SitePatterns[o.Site], v)
			}
			if !patterns[v] {
				family.Patterns = append(family.Patterns, v)
			}
			localPatterns[v] = true
			patterns[v] = true
		}
		for _, v := range o.Destinations {
			if !destinations[v] {
				newSites++
				family.Destinations = append(family.Destinations, v)
			}
			destinations[v] = true
		}
		if n := len(family.SitePatterns[o.Site]); n > 4096 {
			family.SitePatterns[o.Site] = family.SitePatterns[o.Site][n-4096:]
		}
		redundancy := float64(repeat) / float64(max(1, len(o.Patterns)))
		novelty := float64(newSites+newPatterns) / float64(max(1, len(o.Targets)+len(o.Patterns)))
		urls := map[uint64]bool{}
		for _, v := range family.URLs {
			urls[v] = true
		}
		fresh := 0
		for _, v := range o.Targets {
			if !urls[v] {
				fresh++
				urls[v] = true
				family.URLs = append(family.URLs, v)
			}
		}
		freshLinks := float64(fresh) / float64(max(1, len(o.Targets)))
		if len(family.URLs) > 4096 {
			family.URLs = family.URLs[len(family.URLs)-4096:]
		}

		measurement := TrapMeasurement{CrossSiteLinks: o.CrossSiteLinks, Site: o.Site, URI: o.URI, Redundancy: redundancy, Novelty: novelty, Links: o.Links, FreshLinks: freshLinks}
		family.Observations = append(family.Observations, measurement)
		// Cap both per-site and family samples, retaining newest observations.
		counts := map[string]int{}
		kept := []TrapMeasurement{}
		for n := len(family.Observations) - 1; n >= 0 && len(kept) < p.FamilyWindow; n-- {
			v := family.Observations[n]
			if counts[v.Site] >= p.SiteWindow {
				continue
			}
			counts[v.Site]++
			kept = append(kept, v)
		}
		for a, b := 0, len(kept)-1; a < b; a, b = a+1, b-1 {
			kept[a], kept[b] = kept[b], kept[a]
		}
		family.Observations = kept
		for key := range family.SitePatterns {
			if counts[key] == 0 {
				delete(family.SitePatterns, key)
			}
		}
		// Bounded history for novelty, independent of hostname spelling/order.
		if n := len(family.Patterns); n > 4096 {
			family.Patterns = family.Patterns[n-4096:]
		}
		if n := len(family.Destinations); n > 4096 {
			family.Destinations = family.Destinations[n-4096:]
		}
		qualified := map[string]int{}
		eligibleSites := map[string]bool{}
		for _, v := range kept {
			if v.Redundancy >= p.Redundancy && v.Novelty <= p.Novelty && v.Links >= p.MinimumLinks && v.FreshLinks >= p.FreshLinks && v.CrossSiteLinks >= p.CrossSiteLinks {
				qualified[v.Site]++
			} else {
				qualified[v.Site] = 0
				delete(eligibleSites, v.Site)
			}
			if qualified[v.Site] >= 2*p.MinimumSamples {
				eligibleSites[v.Site] = true
			}
		}
		if len(eligibleSites) >= 3 && p.Automatic {
			for key := range eligibleSites {
				if _, e := tx.Get([]byte("trap/allow/" + key)); e == nil {
					continue
				}
				decision := TrapDecision{CrossSiteLinks: o.CrossSiteLinks, FreshLinks: freshLinks, Site: key, Family: family.ID, At: time.Now().UTC(), Samples: qualified[key], Sites: len(eligibleSites), Similarity: similarity, Redundancy: redundancy, Novelty: novelty, Reason: "repeated_structure_and_discovery_with_low_novelty", Settings: *p}
				if e := write(tx, "trap/blocked/"+key, decision); e != nil {
					return e
				}
				if key == o.Site {
					blocked = true
				}
			}
		}
		return write(tx, "trap/family/"+family.ID, family)
	})
	return blocked, e
}
func (s *Store) TrapDecisions(ctx context.Context) ([]TrapDecision, error) {
	out := []TrapDecision{}
	e := s.view(func(tx *badger.Txn) error {
		it := tx.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek([]byte("trap/blocked/")); it.ValidForPrefix([]byte("trap/blocked/")); it.Next() {
			if e := ctx.Err(); e != nil {
				return e
			}
			var d TrapDecision
			if e := it.Item().Value(func(b []byte) error { return json.Unmarshal(b, &d) }); e != nil {
				return e
			}
			if d.Reason == gatewayReason {
				var cluster GatewayCluster
				if err := read(tx, "trap/gateway/"+d.Family, &cluster); err == nil {
					d.Sites = cluster.Members
					d.Gateway = &cluster
				} else if err != badger.ErrKeyNotFound {
					return err
				}
			}
			out = append(out, d)
		}
		return nil
	})
	return out, e
}
func (s *Store) trapBlocked(tx *badger.Txn, raw string) bool {
	if _, e := tx.Get([]byte("trap/allow/" + site.Key(raw))); e == nil {
		return false
	}
	_, e := tx.Get([]byte("trap/blocked/" + site.Key(raw)))
	return e == nil
}

func (s *Store) ParkTraps(ctx context.Context, cs []Candidate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, candidate := range cs {
		if e := ctx.Err(); e != nil {
			return e
		}
		if e := s.update(func(tx *badger.Txn) error {
			var c Candidate
			if e := read(tx, "u/"+candidate.Request.URI, &c); e == badger.ErrKeyNotFound {
				return nil
			} else if e != nil {
				return e
			}
			if (c.State != "pending" && c.State != "reserved") || !s.trapBlocked(tx, c.Request.URI) {
				return nil
			}
			if c.State == "reserved" {
				if e := tx.Delete([]byte(fmt.Sprintf("reserved/%020d/%s", c.Sequence, c.Request.URI))); e != nil {
					return e
				}
				if e := increment(tx, "meta/reserved", -1); e != nil {
					return e
				}
			}
			if e := schedule(tx, c, true); e != nil {
				return e
			}
			if e := tx.Delete([]byte(index(c))); e != nil {
				return e
			}
			if e := increment(tx, "meta/pending", -1); e != nil {
				return e
			}
			if e := increment(tx, "site_pending/"+site.Key(c.Request.Host), -1); e != nil {
				return e
			}
			if e := increment(tx, "meta/trap_parked", 1); e != nil {
				return e
			}
			c.State = "trap_parked"
			if e := write(tx, "u/"+c.Request.URI, c); e != nil {
				return e
			}
			return write(tx, fmt.Sprintf("trap/parked/%s/%020d/%s", site.Key(c.Request.Host), c.Sequence, c.Request.URI), c.Request.URI)
		}); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) RestoreTrap(ctx context.Context, key string) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	key = site.Key(key)
	if key == "" {
		return fmt.Errorf("site required")
	}
	return s.policy.Guard(site.HTTPURL(key), func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.update(func(tx *badger.Txn) error {
			if _, e := tx.Get([]byte("trap/allow/" + key)); e == nil {
				return nil
			}
			if _, e := tx.Get([]byte("trap/blocked/" + key)); e != nil {
				if e == badger.ErrKeyNotFound {
					return fmt.Errorf("trap exclusion not found")
				}
				return e
			}
			if e := tx.Delete([]byte("trap/blocked/" + key)); e != nil {
				return e
			}
			if e := write(tx, "meta/park_cursor", "trap/parked/"+key+"/"); e != nil {
				return e
			}
			if e := write(tx, "meta/parked_staged_cursor", "trap/staged/"+key+"/"); e != nil {
				return e
			}
			return write(tx, "trap/allow/"+key, true)
		})
	})
}

func (s *Store) DrainParked(ctx context.Context, limit int) error {
	if err := s.drainParkedDiscoveries(ctx, limit); err != nil {
		return err
	}
	if limit < 1 {
		return fmt.Errorf("parked scan limit must be positive")
	}
	type row struct{ key, uri string }
	rows := []row{}
	cursor := ""
	last := ""
	inspected := 0
	if e := s.view(func(tx *badger.Txn) error {
		if e := read(tx, "meta/park_cursor", &cursor); e != nil && e != badger.ErrKeyNotFound {
			return e
		}
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := tx.NewIterator(opts)
		defer it.Close()
		scan := func(start, stop string) error {
			for it.Seek([]byte(start)); it.ValidForPrefix([]byte("trap/parked/")) && inspected < limit; it.Next() {
				key := string(it.Item().Key())
				if stop != "" && key >= stop {
					break
				}
				last = key
				inspected++
				var uri string
				if e := read(tx, key, &uri); e != nil {
					return e
				}
				if !s.trapBlocked(tx, uri) {
					rows = append(rows, row{key, uri})
				}
			}
			return nil
		}
		if cursor != "" {
			if e := scan(cursor+"\x00", ""); e != nil {
				return e
			}
		}
		if inspected < limit {
			stop := ""
			if cursor != "" {
				stop = cursor + "\x00"
			}
			return scan("trap/parked/", stop)
		}
		return nil
	}); e != nil {
		return e
	}
	if last != "" && last != cursor {
		s.mu.Lock()
		e := s.update(func(tx *badger.Txn) error {
			var current string
			if e := read(tx, "meta/park_cursor", &current); e != nil && e != badger.ErrKeyNotFound {
				return e
			}
			if current == cursor {
				return write(tx, "meta/park_cursor", last)
			}
			return nil
		})
		s.mu.Unlock()
		if e != nil {
			return e
		}
	}
	for _, r := range rows {
		if e := ctx.Err(); e != nil {
			return e
		}
		s.mu.Lock()
		e := s.update(func(tx *badger.Txn) error {
			var c Candidate
			if e := read(tx, "u/"+r.uri, &c); e == badger.ErrKeyNotFound {
				return tx.Delete([]byte(r.key))
			} else if e != nil {
				return e
			}
			var pending, local int
			if e := read(tx, "meta/pending", &pending); e != nil && e != badger.ErrKeyNotFound {
				return e
			}
			siteKey := site.Key(c.Request.Host)
			if e := read(tx, "site_pending/"+siteKey, &local); e != nil && e != badger.ErrKeyNotFound {
				return e
			}
			if pending >= s.params.MaxPending || local >= s.params.MaxPendingPerOrigin {
				return ErrCapacity
			}
			if e := increment(tx, "meta/trap_parked", -1); e != nil {
				return e
			}
			c.State = "pending"
			if e := write(tx, "u/"+r.uri, c); e != nil {
				return e
			}
			if e := schedule(tx, c, false); e != nil {
				return e
			}
			if e := write(tx, index(c), r.uri); e != nil {
				return e
			}
			if e := increment(tx, "meta/pending", 1); e != nil {
				return e
			}
			if e := increment(tx, "site_pending/"+siteKey, 1); e != nil {
				return e
			}
			return tx.Delete([]byte(r.key))
		})
		s.mu.Unlock()
		if e == ErrCapacity {
			continue
		}
		if e != nil {
			return e
		}
	}
	return nil
}

// Excluded discoveries retain their original receipts outside runnable staging.
func (s *Store) drainParkedDiscoveries(ctx context.Context, limit int) error {
	if limit < 1 {
		return fmt.Errorf("parked scan limit must be positive")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateBoundedParkedDiscoveries(ctx, limit)
}
func (s *Store) updateBoundedParkedDiscoveries(ctx context.Context, limit int) error {
	_, err := s.updateBounded(limit, func(tx *badger.Txn, n int) (int, error) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		cursor, last := "", ""
		if err := read(tx, "meta/parked_staged_cursor", &cursor); err != nil && err != badger.ErrKeyNotFound {
			return 0, err
		}
		inspected := 0
		options := badger.DefaultIteratorOptions
		options.PrefetchValues = false
		it := tx.NewIterator(options)
		defer it.Close()
		scan := func(start, stop string) error {
			for it.Seek([]byte(start)); it.ValidForPrefix([]byte("trap/staged/")) && inspected < n; it.Next() {
				key := string(it.Item().Key())
				if stop != "" && key >= stop {
					break
				}
				last = key
				inspected++
				var request message.FetcherRequest
				if err := read(tx, key, &request); err != nil {
					return err
				}
				if s.trapBlocked(tx, request.URI) {
					continue
				}
				original, err := parkedDiscoveryKey(key)
				if err != nil {
					return err
				}
				if err = write(tx, original, request); err != nil {
					return err
				}
				if s.hasRepeated.Load() {
					marker, _, e := chunkMarker(original)
					if e != nil {
						return e
					}
					if e = s.routeRepeatedChunk(tx, marker); e != nil {
						return e
					}
				}
				if err = tx.Delete([]byte(key)); err != nil {
					return err
				}
				if err = increment(tx, "meta/staged", 1); err != nil {
					return err
				}
				if err = increment(tx, "meta/trap_parked", -1); err != nil {
					return err
				}
			}
			return nil
		}
		if cursor != "" {
			if err := scan(cursor+"\x00", ""); err != nil {
				return 0, err
			}
		}
		if inspected < n {
			stop := ""
			if cursor != "" {
				stop = cursor + "\x00"
			}
			if err := scan("trap/staged/", stop); err != nil {
				return 0, err
			}
		}
		if last != "" && last != cursor {
			if err := write(tx, "meta/parked_staged_cursor", last); err != nil {
				return 0, err
			}
		}
		return inspected, nil
	})
	return err
}
func parkedDiscoveryKey(key string) (string, error) {
	parts := strings.SplitN(key, "/", 4)
	if len(parts) != 4 || !strings.HasPrefix(parts[3], "staged/") {
		return "", fmt.Errorf("invalid parked discovery key")
	}
	return parts[3], nil
}
