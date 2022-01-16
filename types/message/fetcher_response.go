package message

import "github.com/iakinsey/delver/types"

type FetcherResponse struct {
	FetcherRequest

	StoreKey      types.UUID          `json:"store_key,omitempty"`
	ContentMD5    string              `json:"content_md5,omitempty"`
	ElapsedTimeMs int64               `json:"elapsed_time_ms,omitempty"`
	Error         string              `json:"error,omitempty"`
	Header        map[string][]string `json:"header,omitempty"`
	HTTPCode      int                 `json:"http_code,omitempty"`
	Success       bool                `json:"success,omitempty"`
	Timestamp     int64               `json:"timestamp,omitempty"`
}
