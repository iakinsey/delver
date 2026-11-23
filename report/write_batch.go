package report

import (
	"context"
	"database/sql"
	"sync"
)

const maxWaitingWrites = 128

type reportWrite struct {
	ctx  context.Context
	db   *sql.DB
	fn   func(*sql.Tx) error
	done chan error
}

type waitingWrites struct {
	sync.Mutex
	jobs    []*reportWrite
	running bool
	slots   chan struct{}
}

// Queue only concurrent callers. The first caller starts a writer immediately;
// there is no timer or intentional wait for a batch to fill.
func (s *Store) coalesced(ctx context.Context, fn func(*sql.Tx) error) error {
	w := s.writer
	w.waiting.Lock()
	if w.waiting.slots == nil {
		w.waiting.slots = make(chan struct{}, maxWaitingWrites)
	}
	slots := w.waiting.slots
	w.waiting.Unlock()
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-slots }()
	w.waiting.Lock()
	if !w.waiting.running {
		w.waiting.running = true
		w.waiting.Unlock()
		// The uncontended path stays in the caller, including cancellation and
		// acknowledgment. Only callers that accumulated during it need a pump.
		w.Lock()
		err := transaction(ctx, s.db, fn)
		w.Unlock()
		w.waiting.Lock()
		if len(w.waiting.jobs) == 0 {
			w.waiting.running = false
		} else {
			go w.flushWaiting()
		}
		w.waiting.Unlock()
		return err
	}
	job := &reportWrite{ctx: ctx, db: s.db, fn: fn, done: make(chan error, 1)}
	w.waiting.jobs = append(w.waiting.jobs, job)
	w.waiting.Unlock()
	// A queued caller waits for commit/rollback, even when cancellation races a
	// commit. Returning early would release its protection write fence too soon.
	return <-job.done
}

func transaction(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (w *ledgerWriter) flushWaiting() {
	for {
		w.waiting.Lock()
		jobs := w.waiting.jobs
		w.waiting.jobs = nil
		if len(jobs) == 0 {
			w.waiting.running = false
			w.waiting.Unlock()
			return
		}
		w.waiting.Unlock()
		w.Lock()
		failedStatement := false
		skipped := map[*reportWrite]error{}
		err := transaction(context.Background(), jobs[0].db, func(tx *sql.Tx) error {
			for _, job := range jobs {
				if err := job.ctx.Err(); err != nil {
					skipped[job] = err
					continue
				}
				if err := job.fn(tx); err != nil {
					failedStatement = true
					return err
				}
			}
			return nil
		})
		for _, job := range jobs {
			result := err
			if skipped[job] != nil {
				result = skipped[job]
			}
			if failedStatement && job.ctx.Err() == nil {
				// The shared transaction rolled back completely. Preserve ordinary
				// per-call error semantics when one statement/caller failed.
				result = transaction(job.ctx, job.db, job.fn)
			}
			job.done <- result
		}
		w.Unlock()
	}
}
