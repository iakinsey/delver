package message

import "github.com/iakinsey/delver/types"

type FetcherResponse struct {
	FetcherRequest

	StoreKey      types.UUID          `json:"store_key"`
	ContentMD5    string              `json:"content_md5"`
	ElapsedTimeMs int64               `json:"elapsed_time_ms"`
	Error         string              `json:"error"`
	Header        map[string][]string `json:"header"`
	HTTPCode      int                 `json:"http_code"`
	Success       bool                `json:"success"`
	Timestamp     int64               `json:"timestamp"`
}
