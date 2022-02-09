package util

import (
	"sync"

	log "github.com/sirupsen/logrus"
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
	mut := sync.Mutex{}
	mut_, _ := s.mutexes.LoadOrStore(key, &mut)
	mutCast := mut_.(*sync.Mutex)

	mutCast.Lock()

	if mutCast != &mut {
		mutCast.Unlock()
		s.Lock(key)
	}
}

func (s *KeyedMutex) Unlock(key interface{}) {
	if mut, ok := s.mutexes.Load(key); ok {
		mut.(*sync.Mutex).Unlock()
		s.mutexes.Delete(key)
	} else {
		log.Panicf("attempted to unlock mutex with no key loaded %s", key)
	}
}
