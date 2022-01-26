package robots

type Robots interface {
	IsAllowed(url string) (bool, error)
}
