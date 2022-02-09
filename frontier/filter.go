package frontier

type Filter interface {
	IsAllowed(url string) (bool, error)
}
