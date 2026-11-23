//go:build !linux

package fetcher

func newMappedBody(int64) (*mappedBody, error) { return nil, errMappedBodyUnavailable }
