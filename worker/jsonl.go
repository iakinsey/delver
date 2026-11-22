package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/types"
	"os"
	"path/filepath"
	"sync"
)

type JSONLParams struct {
	Path string `json:"path"`
}
type jsonlWorker struct {
	closed bool
	mu     sync.Mutex
	file   *os.File
}

func NewJSONLWorker(params JSONLParams) (Worker, error) {
	if err := os.MkdirAll(filepath.Dir(params.Path), 0755); err != nil {
		return nil, err
	}
	if info, e := os.Lstat(params.Path); e == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("results destination is not a regular file")
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
func (s *jsonlWorker) OnComplete() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.file.Close()
		s.closed = true
	}
}

func (s *jsonlWorker) PurgeMatching(ctx context.Context, match func(string) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.file.Name()
	if s.closed {
		return protection.RewriteJSONL(ctx, path, match)
	}
	if e := s.file.Sync(); e != nil {
		return e
	}
	if e := protection.RewriteJSONL(ctx, path, match); e != nil {
		return e
	}
	if e := s.file.Close(); e != nil {
		return e
	}
	var e error
	s.file, e = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	return e
}
