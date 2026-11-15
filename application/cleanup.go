package application

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type cleanupErrors struct {
	mu     sync.Mutex
	errors []string
}

func (c *cleanupErrors) run(name string, action func() error) {
	var err error
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
		}
		if err != nil {
			c.mu.Lock()
			c.errors = append(c.errors, name+": "+err.Error())
			c.mu.Unlock()
		}
	}()
	err = action()
}
func (c *cleanupErrors) err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.errors) == 0 {
		return nil
	}
	sort.Strings(c.errors)
	return fmt.Errorf("cleanup failed: %s", strings.Join(c.errors, "; "))
}
