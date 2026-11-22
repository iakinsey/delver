package frontier

import (
	"errors"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/flow"
	"time"
)

func (s *Store) SetFlow(controller *flow.Controller) { s.controller.Store(controller) }
func (s *Store) measure(start time.Time, err error) {
	if value := s.controller.Load(); value != nil {
		c := value.(*flow.Controller)
		c.Measure("frontier_storage", time.Since(start))
		c.Counter("storage_operations", 1)
		if errors.Is(err, badger.ErrConflict) {
			c.Counter("storage_conflicts", 1)
		} else if err != nil && !errors.Is(err, badger.ErrTxnTooBig) && err != ErrCooling && err != ErrCapacity && err != ErrRejected && err != ErrDispatchBudget && err != ErrTrapExcluded {
			c.Counter("storage_failures", 1)
		}
	}
}
func (s *Store) update(fn func(*badger.Txn) error) error {
	start := time.Now()
	err := s.db.Update(fn)
	s.measure(start, err)
	return err
}
func (s *Store) view(fn func(*badger.Txn) error) error {
	start := time.Now()
	err := s.db.View(fn)
	s.measure(start, err)
	return err
}
