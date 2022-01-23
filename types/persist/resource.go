package persist

import (
	"io"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
)

type Resource struct {
	RequestID     types.UUID          `json:"request_id,omitempty"`
	URI           string              `json:"uri,omitempty"`
	Host          string              `json:"host,omitempty"`
	Origin        string              `json:"origin,omitempty"`
	Protocol      types.Protocol      `json:"protocol,omitempty"`
	StoreKey      types.UUID          `json:"store_key,omitempty"`
	ContentMD5    string              `json:"content_md5,omitempty"`
	ElapsedTimeMs int64               `json:"elapsed_time_ms,omitempty"`
	Error         string              `json:"error,omitempty"`
	HTTPCode      int                 `json:"http_code,omitempty"`
	Success       bool                `json:"success,omitempty"`
	Timestamp     int64               `json:"timestamp,omitempty"`
	Header        map[string][]string `json:"header,omitempty"`
}

func NewResource(composite message.CompositeAnalysis) *Resource {
	return &Resource{
		RequestID:     composite.RequestID,
		URI:           composite.URI,
		Host:          composite.Host,
		Origin:        composite.Origin,
		Protocol:      composite.Protocol,
		StoreKey:      composite.StoreKey,
		ContentMD5:    composite.ContentMD5,
		ElapsedTimeMs: composite.ElapsedTimeMs,
		Error:         composite.Error,
		HTTPCode:      composite.HTTPCode,
		Success:       composite.Success,
		Timestamp:     composite.Timestamp,
		Header:        composite.Header,
	}
}

func CompositeToResourceParquet(composite message.CompositeAnalysis) (io.Reader, error) {
	resource := NewResource(composite)

	return util.ToParquet(string(composite.RequestID), ResourceParquetSchema, resource)
}

var ResourceParquetSchema = `{
	"Tag": "name=resource, repetitiontype=REQUIRED",
	"Fields": [
		{"Tag": "name=request_id, inname=RequestID, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=uri, inname=URI, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=host, inname=Host, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=origin, inname=Origin, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=protocol, inname=Protocol, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=store_key, inname=StoreKey, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=content_md5, inname=ContentMD5, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=elapsed_time_ms, inname=ElapsedTimeMs, type=INT64, convertedtype=INT_64, repetitiontype=REQUIRED"},
		{"Tag": "name=error, inname=Error, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=http_code, inname=HTTPCode, type=INT32, convertedtype=UINT_16, repetitiontype=REQUIRED"},
		{"Tag": "name=success, inname=Success, type=BOOLEAN, repetitiontype=REQUIRED"},
		{"Tag": "name=timestamp, inname=Timestamp, type=INT64, convertedtype=INT_64, repetitiontype=REQUIRED"},
		{
			"Tag": "name=header, inname=Header, type=MAP, repetitiontype=REQUIRED",
			"Fields": [
				{"Tag": "name=key, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
				{
					"Tag": "name=value, type=LIST, repetitiontype=REQUIRED",
					"Fields": [
						{"Tag": "name=element, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"}
					]
				}
			]
		}
	]
}`
