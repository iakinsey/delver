package frontier

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/trap"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var ErrDispatchBudget = errors.New("request budget reached")
var ErrRejected = errors.New("frontier admission budget exceeded")
var ErrCapacity = errors.New("frontier capacity reached")
var ErrCooling = errors.New("origin cooling")
var ErrTrapExcluded = errors.New("site excluded as crawler trap")

type StoreParams struct {
	Traps               *trap.Settings `json:"traps,omitempty"`
	ExcludedExtensions  []string       `json:"excluded_extensions,omitempty"`
	Retry               *RetryPolicy   `json:"retry,omitempty"`
	RevisitAfter        Lifetime       `json:"revisit_after"`
	MaxDispatches       int64          `json:"max_dispatches"`
	CacheBudgetMB       int            `json:"cache_budget_mb"`
	MaxURLs             int            `json:"max_urls"`
	MaxURLsPerOrigin    int            `json:"max_urls_per_origin"`
	PatternBudgets      map[string]int `json:"pattern_budgets"`
	OriginDelay         Lifetime       `json:"origin_delay"`
	MinFreeBytes        uint64         `json:"min_free_bytes"`
	MaxPendingPerOrigin int            `json:"max_pending_per_origin"`
	Path                string         `json:"path"`
	Retention           Retention      `json:"retention"`
	MaxPending          int            `json:"max_pending"`
}
type Candidate struct {
	ExclusionPolicy string                 `json:"exclusion_policy,omitempty"`
	NextAttempt     int64                  `json:"next_attempt,omitempty"`
	LastFailure     string                 `json:"last_failure,omitempty"`
	Discovered      int64                  `json:"discovered"`
	Request         message.FetcherRequest `json:"request"`
	State           string                 `json:"state"`
	Sequence        int64                  `json:"sequence"`
	Terminal        int64                  `json:"terminal,omitempty"`
}
type RewardPolicy interface {
	Reward(message.FetcherResponse, bool) float64
}
type HTMLReward struct{}

func (HTMLReward) Reward(r message.FetcherResponse, unique bool) float64 {
	if !unique {
		return 0
	}
	elapsed := r.ElapsedTimeMs
	if elapsed < 1 {
		elapsed = 1
	}
	return 1 / float64(elapsed)
}

type Store struct {
	policy          *protection.Policy
	exclusionPolicy string
	controller      atomic.Value
	RewardPolicy    RewardPolicy
	patterns        map[string]*regexp.Regexp
	db              *badger.DB
	mu              sync.Mutex
	params          StoreParams
	healthMu        sync.Mutex
	lastError       string
}

func Normalize(raw, base string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if base != "" {
		b, e := url.Parse(base)
		if e != nil {
			return "", e
		}
		u = b.ResolveReference(u)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return "", fmt.Errorf("invalid crawl URL")
	}
	if (u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443") {
		u.Host = u.Hostname()
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}
func OpenStore(p StoreParams) (*Store, error) {
	if p.Traps != nil {
		if err := p.Traps.Validate(); err != nil {
			return nil, err
		}
	}
	extensions, err := util.NormalizeExtensions(strings.Join(p.ExcludedExtensions, ","))
	if err != nil {
		return nil, err
	}
	p.ExcludedExtensions = extensions
	if p.Retry != nil {
		if err := p.Retry.Validate(); err != nil {
			return nil, err
		}
	}
	for pattern, limit := range p.PatternBudgets {
		if _, err := regexp.Compile(pattern); err != nil {
			return nil, err
		}
		if limit < 1 {
			return nil, fmt.Errorf("pattern budget must be positive")
		}
	}
	if p.MaxURLs < 0 || p.MaxURLsPerOrigin < 0 || p.MaxPendingPerOrigin < 0 {
		return nil, fmt.Errorf("negative frontier budget")
	}
	if p.Path == "" {
		return nil, fmt.Errorf("frontier path required")
	}
	if p.Retention == (Retention{}) {
		p.Retention = DefaultRetention()
	}
	if err := p.Retention.Validate(); err != nil {
		return nil, err
	}
	if p.OriginDelay == 0 && p.Retry == nil {
		p.OriginDelay = Lifetime(100 * time.Millisecond)
	}
	if p.MaxPendingPerOrigin == 0 {
		p.MaxPendingPerOrigin = 10000
	}
	if p.MinFreeBytes == 0 {
		p.MinFreeBytes = 64 << 20
	}
	if p.MaxPending == 0 {
		p.MaxPending = 100000
	}
	if p.MaxPending < 1 {
		return nil, fmt.Errorf("max_pending must be positive")
	}
	if p.CacheBudgetMB == 0 {
		p.CacheBudgetMB = 64
	}
	if p.CacheBudgetMB < 32 || p.CacheBudgetMB > 4096 {
		return nil, fmt.Errorf("cache_budget_mb must be between 32 and 4096")
	}
	db, err := badger.Open(frontierOptions(p.Path, p.CacheBudgetMB))
	if err != nil {
		return nil, err
	}
	patterns := map[string]*regexp.Regexp{}
	for pattern := range p.PatternBudgets {
		patterns[pattern] = regexp.MustCompile(pattern)
	}
	policy := append([]string(nil), p.ExcludedExtensions...)
	sort.Strings(policy)
	policyID := sha256.Sum256([]byte(strings.Join(policy, ",")))
	store := &Store{db: db, params: p, patterns: patterns, RewardPolicy: HTMLReward{}, exclusionPolicy: fmt.Sprintf("%x", policyID)}
	if err := store.ensureSiteIndexes(); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.ensureSiteRewards(); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.ensureFreshIndexes(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}
func (s *Store) SetProtection(p *protection.Policy) { s.policy = p }
func (s *Store) Close() error                       { s.mu.Lock(); defer s.mu.Unlock(); return s.db.Close() }
func read(tx *badger.Txn, key string, v interface{}) error {
	item, err := tx.Get([]byte(key))
	if err != nil {
		return err
	}
	return item.Value(func(b []byte) error { return json.Unmarshal(b, v) })
}
func write(tx *badger.Txn, key string, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return tx.Set([]byte(key), b)
}
func index(c Candidate) string { return fmt.Sprintf("p/%020d/%s", c.Sequence, c.Request.URI) }

// DispatchedRequest supplies the current identity for startup delivery repair.
// It is a point lookup; completed history and pending reservations are excluded.
func (s *Store) DispatchedRequest(uri string) (message.FetcherRequest, bool, error) {
	var request message.FetcherRequest
	found := false
	uri, err := Normalize(uri, "")
	if err != nil {
		return request, false, err
	}
	err = s.view(func(tx *badger.Txn) error {
		var c Candidate
		if err := read(tx, "u/"+uri, &c); err == badger.ErrKeyNotFound {
			return nil
		} else if err != nil {
			return err
		}
		if c.State == "dispatched" && c.Terminal == 0 {
			request, found = c.Request, true
		}
		return nil
	})
	return request, found, err
}

func (s *Store) Admit(ctx context.Context, r message.FetcherRequest) error {
	_, err := s.admit(ctx, r)
	if errors.Is(err, ErrTrapExcluded) {
		return nil
	}
	return err
}

// admit reports whether the committed transaction consumed pending capacity.
// The transaction remains authoritative when concurrent callers share a snapshot.
func (s *Store) admit(ctx context.Context, r message.FetcherRequest) (used bool, err error) {
	if e := ctx.Err(); e != nil {
		return false, e
	}
	if r.Depth < 0 || r.Hops < 0 {
		return false, fmt.Errorf("negative traversal depth")
	}
	uri, e := Normalize(r.URI, "")
	if e != nil {
		return false, e
	}
	r.URI = uri
	err = s.policy.Guard(uri, func() error { used, err = s.admitGuarded(ctx, r); return err })
	if errors.Is(err, protection.ErrBlocked) {
		return false, nil
	}
	return used, err
}
func (s *Store) admitGuarded(ctx context.Context, r message.FetcherRequest) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if r.Depth < 0 || r.Hops < 0 {
		return false, fmt.Errorf("negative traversal depth")
	}
	uri, err := Normalize(r.URI, "")
	if err != nil {
		return false, err
	}
	r.URI = uri
	s.mu.Lock()
	defer s.mu.Unlock()
	if util.URLFileExcluded(uri, s.params.ExcludedExtensions) {
		return false, s.update(func(tx *badger.Txn) error { return increment(tx, "meta/policy_skips", 1) })
	}
	if err := s.checkDisk(); err != nil {
		return false, err
	}
	usedSlot := false
	err = s.update(func(tx *badger.Txn) error { return s.admitTx(tx, r, &usedSlot) })
	return usedSlot && err == nil, err
}

// admitTx must be rolled back on every error: scheduling and budget mutations
// may precede a capacity rejection.
func (s *Store) admitTx(tx *badger.Txn, r message.FetcherRequest, usedSlot *bool) error {
	uri := r.URI

	if s.trapBlocked(tx, uri) {
		return ErrTrapExcluded
	}
	var existing Candidate
	if e := read(tx, "u/"+uri, &existing); e == nil {
		if existing.State == "excluded" && existing.ExclusionPolicy == s.exclusionPolicy {
			return nil
		}
		if existing.State != "excluded" && (!r.BetterTraversalThan(existing.Request) || existing.State == "dedup" || existing.State == "failed") {
			return nil
		}
		if existing.State == "retry_wait" {
			existing.Request.Depth = r.Depth
			existing.Request.Hops = r.Hops
			existing.Request.ScopeRoot = r.ScopeRoot
			existing.Request.Origin = r.Origin
			return write(tx, "u/"+uri, existing)
		}
		if existing.State == "reserved" {
			if err := tx.Delete([]byte(fmt.Sprintf("reserved/%020d/%s", existing.Sequence, existing.Request.URI))); err != nil {
				return err
			}
			if err := increment(tx, "meta/reserved", -1); err != nil {
				return err
			}
		}
		if err := schedule(tx, existing, true); err != nil {
			return err
		}
		if existing.Terminal != 0 {
			var pending, origin int
			if err := read(tx, "meta/pending", &pending); err != nil && err != badger.ErrKeyNotFound {
				return err
			}
			if err := read(tx, "site_pending/"+util.SiteKey(existing.Request.Host), &origin); err != nil && err != badger.ErrKeyNotFound {
				return err
			}
			if pending >= s.params.MaxPending || origin >= s.params.MaxPendingPerOrigin {
				return ErrCapacity
			}
			for _, pair := range []struct {
				life Lifetime
				kind string
			}{{s.params.Retention.CompletedFor, "detail"}, {s.params.Retention.FailedFor, "detail"}, {s.params.Retention.DedupFor, "dedup"}} {
				if pair.life != 0 {
					if err := tx.Delete([]byte(expiry(retentionDeadline(existing.Terminal, pair.life), pair.kind, uri))); err != nil {
						return err
					}
				}
			}
			if err := increment(tx, "meta/pending", 1); err != nil {
				return err
			}
			if err := increment(tx, "site_pending/"+util.SiteKey(existing.Request.Host), 1); err != nil {
				return err
			}
			*usedSlot = true
		}
		if existing.State == "excluded" {
			host := existing.Request.Host
			existing.Request = r
			existing.Request.URI = uri
			existing.Request.Host = host
			existing.Request.Protocol = types.ProtocolHTTP
			existing.Request.RequestID = types.NewV4()
			existing.Request.FrontierGeneration = existing.Request.RequestID
			existing.Request.Attempt = 0
			existing.ExclusionPolicy = ""
			existing.LastFailure = ""
			existing.NextAttempt = 0
			existing.Discovered = time.Now().UnixNano()
		}
		existing.Request.Depth = r.Depth
		existing.Request.Hops = r.Hops
		existing.Terminal = 0
		existing.State = "pending"
		if err := write(tx, "u/"+uri, existing); err != nil {
			return err
		}
		if err := schedule(tx, existing, false); err != nil {
			return err
		}
		return write(tx, index(existing), uri)
	} else if e != badger.ErrKeyNotFound {
		return e
	}
	u, _ := url.Parse(uri)
	budgets := map[string]int{"budget/total": s.params.MaxURLs, "budget/site/" + util.SiteKey(u.Host): s.params.MaxURLsPerOrigin}
	for pattern, limit := range s.params.PatternBudgets {
		if s.patterns[pattern].MatchString(uri) {
			budgets["budget/pattern/"+pattern] = limit
		}
	}
	for key, limit := range budgets {
		if limit == 0 {
			continue
		}
		var count int
		if e := read(tx, key, &count); e != nil && e != badger.ErrKeyNotFound {
			return e
		}
		if count >= limit {
			return ErrRejected
		}
		if err := write(tx, key, count+1); err != nil {
			return err
		}
	}
	var n int
	if e := read(tx, "meta/pending", &n); e != nil && e != badger.ErrKeyNotFound {
		return e
	}
	if n >= s.params.MaxPending {
		return ErrCapacity
	}
	var seq int64
	if e := read(tx, "meta/sequence", &seq); e != nil && e != badger.ErrKeyNotFound {
		return e
	}
	seq++
	if r.RequestID == "" {
		r.RequestID = types.NewV4()
	}
	r.FrontierGeneration = r.RequestID
	u, _ = url.Parse(uri)
	r.Host = u.Host
	r.Protocol = types.ProtocolHTTP
	var originCount int
	if e := read(tx, "site_pending/"+util.SiteKey(r.Host), &originCount); e != nil && e != badger.ErrKeyNotFound {
		return e
	}
	if originCount >= s.params.MaxPendingPerOrigin {
		return ErrCapacity
	}
	if err := write(tx, "site_pending/"+util.SiteKey(r.Host), originCount+1); err != nil {
		return err
	}
	c := Candidate{Request: r, State: "pending", Sequence: seq, Discovered: time.Now().UnixNano()}
	if err := write(tx, "u/"+uri, c); err != nil {
		return err
	}
	if err := schedule(tx, c, false); err != nil {
		return err
	}
	if err := write(tx, index(c), uri); err != nil {
		return err
	}
	if err := write(tx, "meta/sequence", seq); err != nil {
		return err
	}
	*usedSlot = true
	return write(tx, "meta/pending", n+1)
}

// Pending resumes strictly after a key. Deleting that key does not invalidate the cursor.
func (s *Store) Pending(ctx context.Context, after string, limit int) ([]Candidate, string, error) {
	var out []Candidate
	cursor := after
	err := s.view(func(tx *badger.Txn) error {
		it := tx.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		start := []byte("p/")
		if after != "" {
			start = []byte(after)
		}
		for it.Seek(start); it.ValidForPrefix([]byte("p/")) && len(out) < limit; it.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}
			key := string(it.Item().Key())
			if key == after {
				continue
			}
			var uri string
			if err := it.Item().Value(func(b []byte) error { return json.Unmarshal(b, &uri) }); err != nil {
				return err
			}
			var c Candidate
			if err := read(tx, "u/"+uri, &c); err != nil {
				return err
			}
			out = append(out, c)
			cursor = key
		}
		return nil
	})
	return out, cursor, err
}
func (s *Store) Reserve(ctx context.Context, c Candidate) (Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return c, err
	}
	err := s.update(func(tx *badger.Txn) error {
		if err := read(tx, "u/"+c.Request.URI, &c); err != nil {
			return err
		}
		if c.State != "pending" && c.State != "reserved" {
			return fmt.Errorf("candidate unavailable")
		}
		if s.trapBlocked(tx, c.Request.URI) {
			return ErrTrapExcluded
		}
		if c.State == "pending" {
			var dispatched int64
			if e := read(tx, "meta/dispatched", &dispatched); e != nil && e != badger.ErrKeyNotFound {
				return e
			}
			if s.params.MaxDispatches > 0 && dispatched >= s.params.MaxDispatches {
				return ErrDispatchBudget
			}
			var next int64
			if e := read(tx, "cool/"+c.Request.Host, &next); e != nil && e != badger.ErrKeyNotFound {
				return e
			}
			if next > time.Now().UnixNano() {
				return ErrCooling
			}
			delay := time.Duration(s.params.OriginDelay)
			if p := s.params.Retry; p != nil {
				var failures int
				read(tx, "failures/"+c.Request.Host, &failures)
				if failures >= p.FailureThreshold {
					delay = p.ProbeInterval.Value()
				}
			}
			if err := writeTTL(tx, "cool/"+c.Request.Host, time.Now().Add(delay).UnixNano(), delay+time.Second); err != nil {
				return err
			}
		}
		if c.State == "pending" {
			if err := write(tx, fmt.Sprintf("reserved/%020d/%s", c.Sequence, c.Request.URI), c.Request.URI); err != nil {
				return err
			}
			if err := increment(tx, "meta/reserved", 1); err != nil {
				return err
			}
			if err := increment(tx, "meta/dispatched", 1); err != nil {
				return err
			}
		}
		c.State = "reserved"
		return write(tx, "u/"+c.Request.URI, c)
	})
	return c, err
}
func (s *Store) Published(c Candidate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.update(func(tx *badger.Txn) error {
		var current Candidate
		if err := read(tx, "u/"+c.Request.URI, &current); err != nil {
			return err
		}
		if current.State != "reserved" {
			return nil
		}
		if err := tx.Delete([]byte(fmt.Sprintf("reserved/%020d/%s", current.Sequence, current.Request.URI))); err != nil {
			return err
		}
		if err := increment(tx, "meta/reserved", -1); err != nil {
			return err
		}
		current.State = "dispatched"
		if err := schedule(tx, current, true); err != nil {
			return err
		}
		if err := tx.Delete([]byte(index(current))); err != nil {
			return err
		}
		return write(tx, "u/"+current.Request.URI, current)
	})
}
func expiry(t int64, kind, uri string) string { return fmt.Sprintf("e/%020d/%s/%s", t, kind, uri) }
func (s *Store) Outcome(r message.FetcherResponse) error {
	if r.FailureClass == "excluded" {
		r.Retryable = false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	uri, err := Normalize(r.URI, "")
	if err != nil {
		return err
	}
	return s.update(func(tx *badger.Txn) error {
		var c Candidate
		if e := read(tx, "u/"+uri, &c); e == badger.ErrKeyNotFound {
			return nil
		} else if e != nil {
			return e
		}
		if c.State == "retry_wait" {
			return nil
		}
		if c.Terminal != 0 || c.Request.RequestID != r.RequestID || c.Request.BetterTraversalThan(r.FetcherRequest) {
			return nil
		}
		if c.State == "reserved" {
			if err := tx.Delete([]byte(fmt.Sprintf("reserved/%020d/%s", c.Sequence, c.Request.URI))); err != nil {
				return err
			}
			if err := increment(tx, "meta/reserved", -1); err != nil {
				return err
			}
		}
		if retried, err := s.scheduleRetry(tx, &c, r); err != nil || retried {
			return err
		}
		var originCount int
		if err := read(tx, "site_pending/"+util.SiteKey(c.Request.Host), &originCount); err != nil {
			return err
		}
		var countErr error
		if originCount <= 1 {
			countErr = tx.Delete([]byte("site_pending/" + util.SiteKey(c.Request.Host)))
		} else {
			countErr = write(tx, "site_pending/"+util.SiteKey(c.Request.Host), originCount-1)
		}
		if err := countErr; err != nil {
			return err
		}
		var score rewardStats
		if err := read(tx, "site_reward/"+util.SiteKey(c.Request.Host), &score); err != nil && err != badger.ErrKeyNotFound {
			return err
		}
		reward := 0.0
		if r.Success && r.HTTPCode >= 200 && r.HTTPCode < 300 && r.ContentMD5 != "" {
			html := false
			for key, values := range r.Header {
				if strings.EqualFold(key, "Content-Type") {
					for _, v := range values {
						if strings.Contains(strings.ToLower(v), "text/html") {
							html = true
						}
					}
				}
			}
			if html {
				if _, e := tx.Get([]byte("hash/" + r.ContentMD5)); e == badger.ErrKeyNotFound {
					reward = 1
					if err := increment(tx, "meta/unique_html", 1); err != nil {
						return err
					}
					entry := badger.NewEntry([]byte("hash/"+r.ContentMD5), nil)
					if s.params.Retention.DedupFor != 0 {
						entry = entry.WithTTL(time.Duration(s.params.Retention.DedupFor))
					}
					if err := tx.SetEntry(entry); err != nil {
						return err
					}
				} else if e != nil {
					return e
				}
				score.LastHash = r.ContentMD5
			}
		}
		policy := s.RewardPolicy
		if policy == nil {
			policy = HTMLReward{}
		}
		score.Value = 0.8*score.Value + 0.2*policy.Reward(r, reward > 0)
		if err := writeTTL(tx, "site_reward/"+util.SiteKey(c.Request.Host), score, time.Duration(s.params.Retention.DedupFor)); err != nil {
			return err
		}
		if err := increment(tx, "meta/outcomes", 1); err != nil {
			return err
		}
		c.Terminal = time.Now().UnixNano()
		if s.params.RevisitAfter > 0 && r.FailureClass != "excluded" {
			if err := write(tx, revisitKey(retentionDeadline(c.Terminal, s.params.RevisitAfter), c.Request.URI), c.Request); err != nil {
				return err
			}
		}
		c.State = "completed"
		life := s.params.Retention.CompletedFor
		if r.FailureClass == "excluded" {
			c.State = "excluded"
			c.ExclusionPolicy = s.exclusionPolicy
			life = s.params.Retention.CompletedFor
			if err := increment(tx, "meta/policy_skips", 1); err != nil {
				return err
			}
		} else if !r.Success || r.HTTPCode >= 400 {
			c.State = "failed"
			if err := increment(tx, "meta/failed_fetches", 1); err != nil {
				return err
			}
			life = s.params.Retention.FailedFor
		}
		if err := schedule(tx, c, true); err != nil {
			return err
		}
		if err := tx.Delete([]byte(index(c))); err != nil {
			return err
		}
		if life != 0 {
			if err := write(tx, expiry(retentionDeadline(c.Terminal, life), "detail", uri), uri); err != nil {
				return err
			}
		}
		if err := write(tx, "u/"+uri, c); err != nil {
			return err
		}
		var n int
		if err := read(tx, "meta/pending", &n); err != nil {
			return err
		}
		return write(tx, "meta/pending", n-1)
	})
}
func (s *Store) Cleanup(ctx context.Context, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count, err := s.updateBounded(s.params.Retention.CleanupBatchSize, func(tx *badger.Txn, batch int) (int, error) {
		count := 0
		it := tx.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek([]byte("e/")); it.ValidForPrefix([]byte("e/")) && count < batch; it.Next() {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			key := string(it.Item().Key())
			var deadline int64
			if _, err := fmt.Sscanf(key, "e/%d/", &deadline); err != nil {
				return 0, err
			}
			if deadline > now.UnixNano() {
				break
			}
			var uri string
			if err := it.Item().Value(func(b []byte) error { return json.Unmarshal(b, &uri) }); err != nil {
				return 0, err
			}
			var c Candidate
			if err := read(tx, "u/"+uri, &c); err != nil {
				return 0, err
			}
			if c.Terminal == 0 {
				return 0, fmt.Errorf("expiration references active work")
			}
			if c.State == "excluded" {
				if err := tx.Delete([]byte("u/" + uri)); err != nil {
					return 0, err
				}
			} else if strings.Contains(key, "/detail/") {
				c.Request = message.FetcherRequest{URI: uri, RequestID: c.Request.RequestID}
				c.State = "dedup"
				if err := write(tx, "u/"+uri, c); err != nil {
					return 0, err
				}
				if d := s.params.Retention.DedupFor; d != 0 {
					if err := write(tx, expiry(retentionDeadline(c.Terminal, d), "dedup", uri), uri); err != nil {
						return 0, err
					}
				}
			} else {
				if err := tx.Delete([]byte("u/" + uri)); err != nil {
					return 0, err
				}
			}
			if err := tx.Delete([]byte(key)); err != nil {
				return 0, err
			}
			count++
		}
		return count, increment(tx, "meta/cleaned", int64(count))
	})
	if err != nil {
		return count, s.storageError(err)
	}
	gcErr := s.db.RunValueLogGC(.5)
	if gcErr != nil && gcErr != badger.ErrNoRewrite {
		return count, s.storageError(gcErr)
	}
	s.storageError(nil)
	return count, nil
}

type rewardStats struct {
	Value    float64
	LastHash string
}

func (s *Store) Scores(cs []Candidate) (map[string]float64, error) {
	scores := map[string]float64{}
	err := s.view(func(tx *badger.Txn) error {
		for _, c := range cs {
			if _, ok := scores[c.Request.Host]; ok {
				continue
			}
			var r rewardStats
			if err := read(tx, "site_reward/"+util.SiteKey(c.Request.Host), &r); err != nil && err != badger.ErrKeyNotFound {
				return err
			}
			scores[c.Request.Host] = r.Value
		}
		return nil
	})
	return scores, err
}
func (s *Store) Counts() (map[string]int64, error) {
	out := map[string]int64{}
	err := s.view(func(tx *badger.Txn) error {
		for field, key := range map[string]string{"trap_parked": "meta/trap_parked", "policy_skips": "meta/policy_skips", "retry_pending": "meta/retry_pending", "retries_scheduled": "meta/retries_scheduled", "host_cooldowns": "meta/host_cooldowns", "pending_and_active": "meta/pending", "deferred_discoveries": "meta/staged", "budget_rejected": "meta/budget_rejected", "capacity_deferrals": "meta/capacity_deferrals", "unique_html": "meta/unique_html", "failed_fetches": "meta/failed_fetches", "dispatched": "meta/dispatched", "reserved": "meta/reserved", "admitted": "meta/sequence", "terminal_outcomes": "meta/outcomes", "cleaned": "meta/cleaned"} {
			var value int64
			if err := read(tx, key, &value); err != nil && err != badger.ErrKeyNotFound {
				return err
			}
			out[field] = value
		}
		return nil
	})
	return out, err
}

func (s *Store) storageError(err error) error {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	if err != nil {
		s.lastError = err.Error()
	} else {
		s.lastError = ""
	}
	return err
}
func (s *Store) Health() string { s.healthMu.Lock(); defer s.healthMu.Unlock(); return s.lastError }
func (s *Store) CleanupInterval() time.Duration {
	return time.Duration(s.params.Retention.CleanupInterval)
}

func writeTTL(tx *badger.Txn, key string, value interface{}, ttl time.Duration) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	entry := badger.NewEntry([]byte(key), b)
	if ttl > 0 {
		entry = entry.WithTTL(ttl)
	}
	return tx.SetEntry(entry)
}

func increment(tx *badger.Txn, key string, delta int64) error {
	var n int64
	if err := read(tx, key, &n); err != nil && err != badger.ErrKeyNotFound {
		return err
	}
	return write(tx, key, n+delta)
}

func retentionDeadline(base int64, life Lifetime) int64 {
	if int64(life) > math.MaxInt64-base {
		return math.MaxInt64
	}
	return base + int64(life)
}

func (s *Store) checkDisk() error {
	var disk syscall.Statfs_t
	if err := syscall.Statfs(s.params.Path, &disk); err != nil {
		return s.storageError(err)
	}
	if disk.Bavail*uint64(disk.Bsize) < s.params.MinFreeBytes {
		return s.storageError(fmt.Errorf("frontier disk headroom exhausted"))
	}
	return nil
}

// Cache budgets bound configured caches and memtables, not total process RSS.
func frontierOptions(path string, budgetMB int) badger.Options {
	budget := int64(budgetMB) << 20
	opts := badger.DefaultOptions(path).WithLogger(nil).WithSyncWrites(true).WithMemTableSize(budget / 8).WithNumMemtables(2).WithBlockCacheSize(budget / 4).WithIndexCacheSize(budget / 4).WithValueThreshold(1024).WithValueLogFileSize(16 << 20)
	// Badger's default table targets grow through the LSM levels. Its allocator
	// pool retains uncompressed construction and compression buffers, so limiting
	// only read caches does not bound write-side memory under sustained admission.
	opts.BaseTableSize = budget / 64
	opts.TableSizeMultiplier = 1
	return opts.WithNumCompactors(2)
}

func (s *Store) ExcludedFileExtensions() []string {
	return append([]string(nil), s.params.ExcludedExtensions...)
}
