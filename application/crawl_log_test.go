package application

import (
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCrawlLogRecordsSuccessFailuresAndBounds(t *testing.T) {
	log := &crawlLog{}
	input := types.Message{Message: json.RawMessage(`{"uri":"http://example.test/"}`)}
	outputs := []types.Message{{MessageType: types.FetcherResponseType, Message: json.RawMessage(`{"success":true,"http_code":200}`)}}
	log.record(input, outputs, nil)
	log.record(input, nil, fmt.Errorf("fetch failed"))
	entries := log.snapshot()
	require.Len(t, entries, 2)
	require.Equal(t, 200, entries[0].Status)
	require.Equal(t, "failed", entries[1].State)
	for i := 0; i < 600; i++ {
		log.record(input, outputs, nil)
	}
	require.Len(t, log.snapshot(), 500)
}
