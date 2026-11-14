package worker

import (
	"encoding/json"
	"github.com/iakinsey/delver/types"
	"os"
	"path/filepath"
	"sync"
)

type JSONLParams struct {
	Path string `json:"path"`
}
type jsonlWorker struct {
	mu   sync.Mutex
	file *os.File
}

func NewJSONLWorker(params JSONLParams) (Worker, error) {
	if err := os.MkdirAll(filepath.Dir(params.Path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(params.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	return &jsonlWorker{file: f}, nil
}
func (s *jsonlWorker) OnMessage(msg types.Message) (interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := json.NewEncoder(s.file).Encode(msg); err != nil {
		return nil, err
	}
	return nil, s.file.Sync()
}
func (s *jsonlWorker) OnComplete() { s.mu.Lock(); defer s.mu.Unlock(); s.file.Close() }
