package persist

import (
	"io"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
)

type URI struct {
	RequestID types.UUID     `json:"request_id,omitempty"`
	URI       string         `json:"uri,omitempty"`
	Host      string         `json:"host,omitempty"`
	Origin    string         `json:"origin,omitempty"`
	Protocol  types.Protocol `json:"protocol,omitempty"`
}

func CompositeToParquetURI(composite message.CompositeAnalysis) (io.Reader, error) {
	uri := URI(composite.FetcherRequest)

	return util.ToParquet(string(composite.RequestID), URIParquetSchema, uri)
}

var URIParquetSchema = `{
	"Tag": "name=resource, repetitiontype=REQUIRED",
	"Fields": [
		{"Tag": "name=request_id, inname=RequestID, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=uri, inname=URI, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=host, inname=Host, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=origin, inname=Origin, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"},
		{"Tag": "name=protocol, inname=Protocol, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"}
	]
}`
