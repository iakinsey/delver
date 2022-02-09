package frontier

import (
	"sync"

	log "github.com/sirupsen/logrus"
)

type multiFilter struct {
	filters []Filter
}

func NewMultiFilter(filters []Filter) Filter {
	return &multiFilter{
		filters: filters,
	}
}

func (s *multiFilter) IsAllowed(url string) (bool, error) {
	var wg sync.WaitGroup
	results := make(chan bool, len(s.filters))

	for _, filter := range s.filters {
		wg.Add(1)

		go func(f Filter) {
			defer wg.Done()

			allowed, err := f.IsAllowed(url)

			if err != nil {
				log.Errorf("frontier filter failed on url %s: %s", url, err)
			}

			results <- allowed
		}(filter)
	}

	wg.Wait()
	close(results)

	for allowed := range results {
		if !allowed {
			return false, nil
		}
	}

	return true, nil
}
