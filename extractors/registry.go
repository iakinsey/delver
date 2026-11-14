package extractors

import "sync"

type Registration struct {
	Requires []string
	Factory  func() Extractor
}

var registryMu sync.RWMutex
var registry = map[string]Registration{}

func Register(name string, requires []string, factory func() Extractor) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = Registration{Requires: append([]string(nil), requires...), Factory: factory}
}
func Registered(name string) (Registration, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	r, ok := registry[name]
	r.Requires = append([]string(nil), r.Requires...)
	return r, ok
}
