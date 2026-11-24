package frontier

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/repetition"
)

type RepeatedRouting struct {
	Format    int    `json:"format"`
	Ready     bool   `json:"ready"`
	Phase     string `json:"phase"`
	Cursor    string `json:"cursor,omitempty"`
	Epoch     int64  `json:"epoch"`
	Target    int64  `json:"target"`
	Completed int64  `json:"completed"`
	Settings  string `json:"settings"`
}
type repeatedChunk struct {
	Normal   [2]uint64 `json:"normal"`
	Repeated [2]uint64 `json:"repeated"`
}

func bitSet(bits *[2]uint64, n int)     { bits[n/64] |= uint64(1) << uint(n%64) }
func bitClear(bits *[2]uint64, n int)   { bits[n/64] &^= uint64(1) << uint(n%64) }
func bitHas(bits [2]uint64, n int) bool { return bits[n/64]&(uint64(1)<<uint(n%64)) != 0 }
func bitAny(bits [2]uint64) bool        { return bits[0] != 0 || bits[1] != 0 }
func (s *Store) initializeRepeated() error {
	settings := s.repeatedSettings()
	settings.Mode = ""
	settings.AdmissionShare = 0
	s.repeatedEpoch = repeatedDigest(struct {
		Version  int
		Settings repetition.Settings
	}{2, settings})
	var state RepeatedRouting
	if err := s.db.View(func(tx *badger.Txn) error { return repeatedRead(tx, "repeated/routing", &state) }); err != nil {
		return err
	}
	if s.repeatedSettings().Mode == "off" && state.Format == 0 {
		return nil
	}
	s.hasRepeated.Store(true)
	var dispatchStep uint64
	if err := s.db.View(func(tx *badger.Txn) error { return repeatedRead(tx, "repeated/dispatch_step", &dispatchStep) }); err != nil {
		return err
	}
	s.repeatedScanStep.Store(dispatchStep)
	return s.update(func(tx *badger.Txn) error {
		hash := repeatedDigest(struct {
			Version  int
			Settings repetition.Settings
		}{2, s.repeatedSettings()})
		changed := state.Format == 0 || state.Settings != hash
		if state.Format == 0 {
			state = RepeatedRouting{Format: 1, Target: 1, Epoch: 1, Phase: "chunks", Settings: hash}
			var pending, staged int64
			if err := repeatedRead(tx, "meta/pending", &pending); err != nil {
				return err
			}
			if err := repeatedRead(tx, "meta/staged", &staged); err != nil {
				return err
			}
			if pending == 0 && staged == 0 {
				state.Ready = true
				state.Phase = ""
				state.Completed = 1
			}
		} else if state.Settings != hash {
			state.Ready = false
			state.Target++
			state.Settings = hash
			if state.Phase == "" {
				state.Phase = "chunks"
				state.Cursor = ""
				state.Epoch = state.Target
			}
		}
		if err := write(tx, "repeated/routing", state); err != nil {
			return err
		}
		if changed {
			return s.enqueueRepeatedProjection(tx, repetition.Update{})
		}
		return nil
	})
}
func (s *Store) markRepeatedRouting(tx *badger.Txn) error {
	var state RepeatedRouting
	if err := repeatedRead(tx, "repeated/routing", &state); err != nil {
		return err
	}
	state.Target++
	if state.Phase == "" {
		state.Phase = "chunks"
		state.Cursor = ""
		state.Epoch = state.Target
	}
	return write(tx, "repeated/routing", state)
}
func chunkIdentity(marker string) (string, int, error) {
	parts := strings.Split(marker, "/")
	if len(parts) != 3 || parts[0] != "discovery" {
		return "", 0, fmt.Errorf("invalid discovery receipt")
	}
	n, e := strconv.Atoi(parts[2])
	return parts[1], n, e
}
func chunkMarker(key string) (string, int, error) {
	parts := strings.Split(key, "/")
	if len(parts) != 3 || parts[0] != "staged" {
		return "", 0, fmt.Errorf("invalid staged identity")
	}
	n, e := strconv.Atoi(parts[2])
	return fmt.Sprintf("discovery/%s/%08d", parts[1], n/128*128), n % 128, e
}
func chunkSuffix(marker string) string { return strings.TrimPrefix(marker, "discovery/") }
func (s *Store) writeChunkRouting(tx *badger.Txn, marker string, c repeatedChunk) error {
	suffix := chunkSuffix(marker)
	for class, bits := range [][2]uint64{c.Normal, c.Repeated} {
		key := fmt.Sprintf("repeated/ready/%d/%s", class, suffix)
		if bitAny(bits) {
			if e := tx.Set([]byte(key), nil); e != nil {
				return e
			}
		} else if e := tx.Delete([]byte(key)); e != nil {
			return e
		}
	}
	key := "repeated/chunk/" + suffix
	if !bitAny(c.Normal) && !bitAny(c.Repeated) {
		return tx.Delete([]byte(key))
	}
	return write(tx, key, c)
}
func (s *Store) routeRepeatedChunk(tx *badger.Txn, marker string, caches ...*repeatedClassifier) error {
	var cache *repeatedClassifier
	if len(caches) > 0 {
		cache = caches[0]
	} else {
		cache = newRepeatedClassifier(s, tx)
	}
	digest, start, e := chunkIdentity(marker)
	if e != nil {
		return e
	}
	var remaining int
	if e := repeatedRead(tx, marker, &remaining); e != nil {
		return e
	}
	c := repeatedChunk{}
	if remaining > 0 {
		for i := 0; i < 128; i++ {
			key := fmt.Sprintf("staged/%s/%08d", digest, start+i)
			var entry discoveryEntry
			if e := read(tx, key, &entry.request); e == badger.ErrKeyNotFound {
				continue
			} else if e != nil {
				return e
			}
			r := Candidate{Request: entry.request}
			repeated, e := s.candidateRepeated(tx, r, cache)
			if e != nil {
				return e
			}
			if repeated {
				bitSet(&c.Repeated, i)
			} else {
				bitSet(&c.Normal, i)
			}
		}
	}
	return s.writeChunkRouting(tx, marker, c)
}
func (s *Store) ackRepeatedChunk(tx *badger.Txn, key string, caches ...*repeatedAcks) error {
	if !s.hasRepeated.Load() {
		return nil
	}
	marker, bit, e := chunkMarker(key)
	if e != nil {
		return e
	}
	var c repeatedChunk
	known := false
	if len(caches) > 0 {
		c, known = caches[0].chunks[marker]
	}
	if !known {
		if e := repeatedRead(tx, "repeated/chunk/"+chunkSuffix(marker), &c); e != nil {
			return e
		}
	}
	bitClear(&c.Normal, bit)
	bitClear(&c.Repeated, bit)
	if len(caches) > 0 {
		caches[0].chunks[marker] = c
		return nil
	}
	return s.writeChunkRouting(tx, marker, c)
}

// MaintainRepeatedRouting never blocks startup. Each transaction updates at
// most 24 receipt chunks (under 128 metadata mutations), or 128 pending rows.
// Payload keys, original receipts, sequences and seek identities are retained.
func (s *Store) MaintainRepeatedRouting(ctx context.Context, limit int) error {
	if !s.hasRepeated.Load() {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	limit = min(128, max(1, limit))
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.update(func(tx *badger.Txn) error {
		cache := newRepeatedClassifier(s, tx)
		var state RepeatedRouting
		if e := read(tx, "repeated/routing", &state); e != nil {
			return e
		}
		if state.Phase == "" {
			return nil
		}
		prefix := "discovery/"
		budget := min(24, limit)
		if state.Phase == "pending" {
			prefix = "p/"
			if s.repeatedSettings().Mode == "off" {
				prefix = "u/"
			}
			budget = limit
		}
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := tx.NewIterator(opts)
		defer it.Close()
		start := prefix
		if state.Cursor != "" {
			start = state.Cursor + "\x00"
		}
		n := 0
		for it.Seek([]byte(start)); it.ValidForPrefix([]byte(prefix)) && n < budget; it.Next() {
			if e := ctx.Err(); e != nil {
				return e
			}
			key := string(it.Item().Key())
			state.Cursor = key
			n++
			if state.Phase == "chunks" {
				if e := s.routeRepeatedChunk(tx, key, cache); e != nil {
					return e
				}
			} else {
				uri := strings.TrimPrefix(key, "u/")
				if prefix == "p/" {
					if e := read(tx, key, &uri); e != nil {
						return e
					}
				}
				var c Candidate
				if e := read(tx, "u/"+uri, &c); e == badger.ErrKeyNotFound {
					continue
				} else if e != nil {
					return e
				}
				pinSeed := c.Request.Origin == "" && !c.NormalPinned
				if pinSeed {
					c.NormalPinned = true
				}
				repeated, e := s.candidateRepeated(tx, c, cache)
				if e != nil {
					return e
				}
				if c.Repeated != repeated || pinSeed {
					if e := schedule(tx, c, true); e != nil {
						return e
					}
					c.Repeated = repeated
					if e := write(tx, "u/"+uri, c); e != nil {
						return e
					}
					if c.State == "pending" || c.State == "reserved" {
						if e := schedule(tx, c, false); e != nil {
							return e
						}
					}
				}
			}
		}
		if !it.ValidForPrefix([]byte(prefix)) {
			state.Cursor = ""
			if state.Phase == "chunks" {
				state.Phase = "pending"
			} else {
				state.Ready = true
				state.Completed = state.Epoch
				state.Phase = ""
				if state.Target > state.Completed {
					state.Phase = "chunks"
					state.Epoch = state.Target
				}
			}
		}
		return write(tx, "repeated/routing", state)
	})
}
func (s *Store) RepeatedAdmissionShare() int {
	if s.repeatedSettings().Mode != "deprioritize" {
		return 0
	}
	return s.repeatedSettings().AdmissionShare
}
func repeatedTurn(step uint64, share int) bool { return repetition.Turn(step, share) }

// Routed discovery readers seek receipt chunks in each class. They never
// materialize the staging backlog or create a URL-level deduplication index.
func (s *Store) repeatedDiscoveryWindow(tx *badger.Txn, limit int) ([]discoveryEntry, bool, error) {
	var state RepeatedRouting
	if e := repeatedRead(tx, "repeated/routing", &state); e != nil {
		return nil, false, e
	}
	if !state.Ready || s.repeatedSettings().Mode != "deprioritize" {
		return nil, false, nil
	}
	lists := [2][]discoveryEntry{}
	opts := badger.DefaultIteratorOptions
	opts.PrefetchValues = false
	for class := 0; class < 2; class++ {
		prefix := fmt.Sprintf("repeated/ready/%d/", class)
		opts.Prefix = []byte(prefix)
		var cursor string
		if e := repeatedRead(tx, fmt.Sprintf("repeated/stage_cursor/%d", class), &cursor); e != nil {
			return nil, false, e
		}
		it := tx.NewIterator(opts)
		cursorKey, cursorBit := cursor, -1
		if parts := strings.Split(cursor, "|"); len(parts) == 2 {
			cursorKey = parts[0]
			cursorBit, _ = strconv.Atoi(parts[1])
		}
		chunks := 0
		scan := func(start string, wrap bool) error {
			for it.Seek([]byte(start)); it.ValidForPrefix([]byte(prefix)) && len(lists[class]) < limit && chunks < limit; it.Next() {
				key := string(it.Item().Key())
				if wrap && cursorKey != "" && key > cursorKey {
					break
				}
				chunks++
				suffix := strings.TrimPrefix(key, prefix)
				digest, first, e := chunkIdentity("discovery/" + suffix)
				if e != nil {
					return e
				}
				var c repeatedChunk
				if e := repeatedRead(tx, "repeated/chunk/"+suffix, &c); e != nil {
					return e
				}
				bits := c.Normal
				if class == 1 {
					bits = c.Repeated
				}
				for i := 0; i < 128 && len(lists[class]) < limit; i++ {
					if key == cursorKey && ((!wrap && i <= cursorBit) || (wrap && i > cursorBit)) {
						continue
					}
					if !bitHas(bits, i) {
						continue
					}
					k := fmt.Sprintf("staged/%s/%08d", digest, first+i)
					entry := discoveryEntry{key: k, route: fmt.Sprintf("%s|%03d", key, i), routeClass: class, repeated: class == 1}
					if e := read(tx, k, &entry.request); e == badger.ErrKeyNotFound {
						entry.stale = true
					} else if e != nil {
						return e
					}
					lists[class] = append(lists[class], entry)
				}
			}
			return nil
		}
		if cursorKey != "" {
			if e := scan(cursorKey, false); e != nil {
				it.Close()
				return nil, false, e
			}
		}
		if len(lists[class]) < limit {
			if e := scan(prefix, cursorKey != ""); e != nil {
				it.Close()
				return nil, false, e
			}
		}

		it.Close()
	}
	var step uint64
	if e := repeatedRead(tx, "repeated/stage_step", &step); e != nil {
		return nil, false, e
	}
	var deferred uint64
	if e := repeatedRead(tx, "repeated/stage_deferral_step", &deferred); e != nil {
		return nil, false, e
	}
	step += deferred
	out := make([]discoveryEntry, 0, limit)
	positions := [2]int{}
	for len(out) < limit {
		class := 0
		if repeatedTurn(step, s.repeatedSettings().AdmissionShare) {
			class = 1
		}
		if positions[class] >= len(lists[class]) {
			class = 1 - class
		}
		if positions[class] >= len(lists[class]) {
			break
		}
		out = append(out, lists[class][positions[class]])
		positions[class]++
		step++
	}
	return out, true, nil
}

func (s *Store) checkpointRepeatedRoute(tx *badger.Txn, e discoveryEntry) error {
	if e.route == "" {
		return nil
	}
	return write(tx, fmt.Sprintf("repeated/stage_cursor/%d", e.routeClass), e.route)
}

// Normal provenance is independent of traversal-path improvements.
func (s *Store) updateRepeatedCandidate(tx *badger.Txn, c *Candidate, rOrigin string, caches ...*repeatedClassifier) error {
	if !s.hasRepeated.Load() {
		return nil
	}
	old := *c
	var cache *repeatedClassifier
	if len(caches) > 0 {
		cache = caches[0]
	} else {
		cache = newRepeatedClassifier(s, tx)
	}
	if err := s.mergeRepeatedOrigins(c, rOrigin, cache); err != nil {
		return err
	}
	repeated := c.Repeated
	if c.Terminal == 0 {
		var e error
		repeated, e = s.candidateRepeated(tx, *c, cache)
		if e != nil {
			return e
		}
	}
	c.Repeated = repeated
	if c.State == "pending" && old.Repeated != repeated {
		if e := schedule(tx, old, true); e != nil {
			return e
		}
		if e := schedule(tx, *c, false); e != nil {
			return e
		}
	}
	if old.Repeated != c.Repeated || old.NormalPinned != c.NormalPinned || len(old.Origins) != len(c.Origins) {
		return write(tx, "u/"+c.Request.URI, *c)
	}
	return nil
}

// Receipt bits are coalesced only inside the already-bounded cleanup commit.
// The payload deletions, counters, original receipts and routing masks remain
// atomic; synchronous ownership acknowledgments still wait for that commit.
type repeatedAcks struct{ chunks map[string]repeatedChunk }

func (s *Store) flushRepeatedAcks(tx *badger.Txn, cache *repeatedAcks) error {
	for marker, c := range cache.chunks {
		if err := s.writeChunkRouting(tx, marker, c); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RepeatedDispatchStep() (uint64, error) {
	var step uint64
	err := s.view(func(tx *badger.Txn) error { return repeatedRead(tx, "repeated/dispatch_step", &step) })
	return step, err
}
