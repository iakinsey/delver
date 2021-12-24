package model

import "github.com/iakinsey/delver/types"

type Response struct {
	Request

	ContentMD5  types.MD5
	ElapsedTime int32
	Error       string
	Header      map[string]string
	HTTPCode    types.HTTPCode
	Partial     bool
	ResponseID  types.UUID
	Success     bool
	Timestamp   int64
}
