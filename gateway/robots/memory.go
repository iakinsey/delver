package robots

type memoryRobots struct{}

func NewMemoryRobots() Robots {
	return &memoryRobots{}
}

func (*memoryRobots) IsAllowed(url string) (bool, error) {
	return false, nil
}
