package frontier

type multiFilter struct {
	filters []Filter
}

func NewMultiFilter(filters []Filter) Filter {
	return &multiFilter{
		filters: filters,
	}
}

func (s *multiFilter) IsAllowed(url string) (bool, error) {
	for _, filter := range s.filters {
		allowed, err := filter.IsAllowed(url)
		if err != nil {
			return false, err
		}
		if !allowed {
			return false, nil
		}
	}
	return true, nil
}
