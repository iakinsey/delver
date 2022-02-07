package util

import (
	"log"
	"sync"
)

type KeyedMutex struct {
	mutexes *sync.Map
}

func NewKeyedMutex() *KeyedMutex {
	return &KeyedMutex{
		mutexes: &sync.Map{},
	}
}

func (s *KeyedMutex) Lock(key interface{}) {
	mut, _ := s.mutexes.LoadOrStore(key, &sync.RWMutex{})

	mut.(*sync.RWMutex).Lock()
}

func (s *KeyedMutex) Unlock(key interface{}) {
	if mut, ok := s.mutexes.Load(key); ok {
		mut.(*sync.RWMutex).Unlock()
		s.mutexes.Delete(key)
	} else {
		log.Panicf("attempted to unlock mutex with no key loaded %s", key)
	}
}
