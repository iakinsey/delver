package application

import (
	"encoding/json"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"sync"
	"time"
)

type CrawlEvent struct {
	ID        string `json:"id"`
	Timestamp int64  `json:"timestamp"`
	URI       string `json:"uri"`
	Status    int    `json:"http_status,omitempty"`
	State     string `json:"state"`
	Error     string `json:"error,omitempty"`
}
type crawlLog struct {
	mu      sync.Mutex
	entries []CrawlEvent
}

func (log *crawlLog) record(input types.Message, outputs []types.Message, err error) {
	var request message.FetcherRequest
	if json.Unmarshal(input.Message, &request) != nil {
		return
	}
	event := CrawlEvent{ID: string(types.NewV4()), Timestamp: time.Now().Unix(), URI: request.URI, State: "fetched"}
	for _, output := range outputs {
		if output.MessageType == types.FetcherResponseType {
			var response message.FetcherResponse
			if json.Unmarshal(output.Message, &response) == nil {
				event.Status = response.HTTPCode
				if !response.Success || response.HTTPCode >= 400 {
					event.State = "failed"
					event.Error = response.Error
				}
			}
		}
	}
	if err != nil {
		event.State = "failed"
		event.Error = err.Error()
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	log.entries = append(log.entries, event)
	if len(log.entries) > 500 {
		log.entries = append([]CrawlEvent(nil), log.entries[len(log.entries)-500:]...)
	}
}
func (log *crawlLog) snapshot() []CrawlEvent {
	log.mu.Lock()
	defer log.mu.Unlock()
	return append([]CrawlEvent{}, log.entries...)
}
