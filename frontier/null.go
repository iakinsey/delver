package frontier

type nullFilter struct{}

func NewNullFilter() Filter {
	return &nullFilter{}
}

func (s *nullFilter) IsAllowed(url string) (bool, error) {
	return true, nil
}
