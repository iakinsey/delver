package message

import "github.com/iakinsey/delver/types"

type FetcherResponse struct {
	FetcherRequest

	StoreKey      types.UUID
	ContentMD5    types.MD5
	ElapsedTimeMs int64
	Error         string
	Header        map[string]string
	HTTPCode      int
	Success       bool
	Timestamp     int64
}
