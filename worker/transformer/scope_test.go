package transformer

import (
	"context"
	"encoding/json"
	"github.com/iakinsey/delver/transformers"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
	"io"
	"testing"
)

type scopeSearch struct{ rows []*types.Indexable }

func (s *scopeSearch) Index(*types.Indexable) error                         { return nil }
func (s *scopeSearch) IndexMany(rows []*types.Indexable) error              { s.rows = rows; return nil }
func (s *scopeSearch) Declare(types.Index)                                  {}
func (s *scopeSearch) Search(io.Reader) ([]json.RawMessage, error)          { return nil, nil }
func (s *scopeSearch) SearchAggregate(io.Reader) ([]json.RawMessage, error) { return nil, nil }

type scopeStream struct{}

func (scopeStream) Start() error                     { return nil }
func (scopeStream) Publish([]*types.Indexable) error { return nil }
func TestIndexedLogsCarryCrawlAndRunOwnership(t *testing.T) {
	search := &scopeSearch{}
	w := &transformer{crawlID: "crawl-a", runID: "run-b", search: search, t: []transformers.Transformer{transformers.NewCompositeTransformer()}}
	_, err := w.OnMessageContext(context.Background(), types.Message{MessageType: types.CompositeAnalysisType, Message: json.RawMessage(`{"request_id":"request-c","timestamp":9223372036854775807,"features":{}}`)})
	require.NoError(t, err)
	require.Len(t, search.rows, 1)
	require.IsType(t, message.CompositeAnalysis{}, search.rows[0].Data)
	document, err := search.rows[0].IndexDocument()
	require.NoError(t, err)
	raw, err := json.Marshal(document)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"delver_crawl_id":"crawl-a"`)
	require.Contains(t, string(raw), `"delver_run_id":"run-b"`)
	require.Contains(t, string(raw), `9223372036854775807`)
}
