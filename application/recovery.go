package application

import (
	"fmt"
	"github.com/iakinsey/delver/queue"
)

func (s *Supervisor) recovery(name string) (queue.Recovery, error) {
	if s.control == nil {
		return nil, fmt.Errorf("queue access requires an active runtime")
	}
	s.control.mu.Lock()
	defer s.control.mu.Unlock()
	if s.control.prepared == nil {
		return nil, fmt.Errorf("runtime is unavailable")
	}
	recovery, ok := s.control.prepared.resources[name].(queue.Recovery)
	if !ok {
		return nil, fmt.Errorf("queue does not support recovery")
	}
	return recovery, nil
}
func (s *Supervisor) failed(name string, offset int) (interface{}, error) {
	if offset < 0 {
		return nil, fmt.Errorf("offset must be nonnegative")
	}
	recovery, err := s.recovery(name)
	if err != nil {
		return nil, err
	}
	records, err := recovery.Failed(offset, 50)
	for i := range records {
		records[i].Error = s.safeError(records[i].Error)
	}
	return records, err
}
func (s *Supervisor) recoverRecord(name, id string, recrawl bool) error {
	recovery, err := s.recovery(name)
	if err != nil {
		return err
	}
	return recovery.Recover(id, recrawl)
}
