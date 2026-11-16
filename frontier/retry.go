package frontier

import (
	"context"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"time"
)

type RetryPolicy struct {
	Share            float64       `json:"share"`
	Limit            int           `json:"limit"`
	Base             flow.Duration `json:"base"`
	Maximum          flow.Duration `json:"maximum"`
	FailureThreshold int           `json:"failure_threshold"`
	HostCooldown     flow.Duration `json:"host_cooldown"`
	HostCooldownMax  flow.Duration `json:"host_cooldown_max"`
	ProbeInterval    flow.Duration `json:"probe_interval"`
}

func (p RetryPolicy) Validate() error {
	if p.Share <= 0 || p.Share > 1 || p.Limit < 0 || p.Limit > 10 || p.Base <= 0 || p.Maximum < p.Base || p.FailureThreshold < 1 || p.HostCooldown <= 0 || p.HostCooldownMax < p.HostCooldown || p.ProbeInterval <= 0 {
		return fmt.Errorf("invalid durable retry policy")
	}
	return nil
}
func retryKey(at int64, uri string) string { return fmt.Sprintf("retry/%020d/%s", at, uri) }
func backoff(base, maximum time.Duration, n int) time.Duration {
	value := base
	for i := 0; i < n && value < maximum; i++ {
		if value > maximum/2 {
			return maximum
		}
		value *= 2
	}
	if value > maximum {
		return maximum
	}
	return value
}

// scheduleRetry is part of the same transaction as observing a fetch outcome.
// Its due index, candidate and counters commit together; replay cannot duplicate it.
func (s *Store) scheduleRetry(tx *badger.Txn, c *Candidate, r message.FetcherResponse) (bool, error) {
	p := s.params.Retry
	if p == nil {
		return false, nil
	}
	if r.Retryable {
		var failures int
		if err := read(tx, "failures/"+c.Request.Host, &failures); err != nil && err != badger.ErrKeyNotFound {
			return false, err
		}
		failures++
		if err := writeTTL(tx, "failures/"+c.Request.Host, failures, time.Duration(s.params.Retention.FailedFor)); err != nil {
			return false, err
		}
		if failures >= p.FailureThreshold {
			delay := backoff(p.HostCooldown.Value(), p.HostCooldownMax.Value(), failures-p.FailureThreshold)
			if err := writeTTL(tx, "cool/"+c.Request.Host, time.Now().Add(delay).UnixNano(), delay+time.Second); err != nil {
				return false, err
			}
			if err := increment(tx, "meta/host_cooldowns", 1); err != nil {
				return false, err
			}
		}
	} else if r.Success {
		if err := tx.Delete([]byte("failures/" + c.Request.Host)); err != nil {
			return false, err
		}
		if err := tx.Delete([]byte("cool/" + c.Request.Host)); err != nil {
			return false, err
		}
	}
	if !r.Retryable || c.Request.Attempt >= p.Limit {
		return false, nil
	}
	if c.State == "retry_wait" {
		return true, nil
	}
	delay := backoff(p.Base.Value(), p.Maximum.Value(), c.Request.Attempt)
	if r.RetryAfterMS > delay.Milliseconds() {
		delay = time.Duration(r.RetryAfterMS) * time.Millisecond
	}
	c.State = "retry_wait"
	c.NextAttempt = time.Now().Add(delay).UnixNano()
	c.LastFailure = r.FailureClass
	if err := schedule(tx, *c, true); err != nil {
		return false, err
	}
	if err := tx.Delete([]byte(index(*c))); err != nil {
		return false, err
	}
	if err := write(tx, retryKey(c.NextAttempt, c.Request.URI), c.Request.URI); err != nil {
		return false, err
	}
	if err := increment(tx, "meta/retry_pending", 1); err != nil {
		return false, err
	}
	if err := increment(tx, "meta/retries_scheduled", 1); err != nil {
		return false, err
	}
	return true, write(tx, "u/"+c.Request.URI, *c)
}
func (s *Store) DrainRetries(ctx context.Context, limit int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.updateBounded(limit, func(tx *badger.Txn, batch int) (int, error) {
		it := tx.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		count := 0
		for it.Seek([]byte("retry/")); it.ValidForPrefix([]byte("retry/")) && count < batch; it.Next() {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			key := string(it.Item().Key())
			var due int64
			if _, err := fmt.Sscanf(key, "retry/%d/", &due); err != nil {
				return 0, err
			}
			if due > time.Now().UnixNano() {
				break
			}
			var uri string
			if err := read(tx, key, &uri); err != nil {
				return 0, err
			}
			var c Candidate
			if err := read(tx, "u/"+uri, &c); err != nil {
				return 0, err
			}
			if c.State == "retry_wait" && c.NextAttempt == due {
				c.State = "pending"
				c.NextAttempt = 0
				c.Request.Attempt++
				c.Request.RequestID = types.NewV4()
				c.Request.FrontierGeneration = types.NewV4()
				if err := write(tx, "u/"+uri, c); err != nil {
					return 0, err
				}
				if err := write(tx, index(c), uri); err != nil {
					return 0, err
				}
				if err := schedule(tx, c, false); err != nil {
					return 0, err
				}
				if err := increment(tx, "meta/retry_pending", -1); err != nil {
					return 0, err
				}
			}
			if err := tx.Delete([]byte(key)); err != nil {
				return 0, err
			}
			count++
		}
		return count, nil
	})
	return err
}
