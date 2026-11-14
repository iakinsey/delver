package util

import "sync"

type keyedLock struct {
	mutex sync.Mutex
	refs  int
}
type KeyedMutex struct {
	mu    sync.Mutex
	locks map[interface{}]*keyedLock
}

func NewKeyedMutex() *KeyedMutex { return &KeyedMutex{locks: make(map[interface{}]*keyedLock)} }
func (s *KeyedMutex) Lock(key interface{}) {
	s.mu.Lock()
	entry := s.locks[key]
	if entry == nil {
		entry = &keyedLock{}
		s.locks[key] = entry
	}
	entry.refs++
	s.mu.Unlock()
	entry.mutex.Lock()
}
func (s *KeyedMutex) Unlock(key interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.locks[key]
	if entry == nil {
		panic("unlock of unknown key")
	}
	entry.mutex.Unlock()
	entry.refs--
	if entry.refs == 0 {
		delete(s.locks, key)
	}
}
