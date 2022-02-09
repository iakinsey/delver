package frontier

type FrontierFilter interface {
	IsAllowed(url string) (bool, error)
}
