package accumulator

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
)

type failMap struct{}

func (failMap) Get([]byte) ([]byte, error)            { return nil, fmt.Errorf("storage unavailable") }
func (failMap) Set([]byte, []byte) error              { return fmt.Errorf("storage unavailable") }
func (failMap) SetMany([][2][]byte) error             { return fmt.Errorf("storage unavailable") }
func (failMap) Iter(func([]byte, []byte) error) error { return nil }
func (failMap) Close()                                {}
func TestDFSStorageFailurePreventsAcknowledgment(t *testing.T) {
	s := NewDfsBasicAccumulator(DfsBasicAccumulatorParams{UrlStore: failMap{}, MaxDepth: 2})
	msg, err := types.NewMessage(message.CompositeAnalysis{FetcherResponse: message.FetcherResponse{Success: true, FetcherRequest: message.FetcherRequest{URI: "http://example.test/"}}, Features: map[string]interface{}{"url": features.URIs{"http://external.test/"}}}, types.CompositeAnalysisType)
	require.NoError(t, err)
	_, err = s.OnMessage(msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "storage unavailable")
}
func TestNewsPublicationCancellationAndStableIdentity(t *testing.T) {
	output := queue.NewChannelQueue()
	defer output.Stop()
	s := NewNewsAccumulator(NewsAccumulatorParams{NewsQueue: output})
	analysis := message.CompositeAnalysis{FetcherResponse: message.FetcherResponse{Success: true, FetcherRequest: message.FetcherRequest{URI: "http://example.test/article", RequestID: "request", Depth: 1}}, Features: map[string]interface{}{"url": features.URIs{}}}
	msg, err := types.NewMessage(analysis, types.CompositeAnalysisType)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = s.(interface {
		OnMessageContext(context.Context, types.Message) (interface{}, error)
	}).OnMessageContext(ctx, msg)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	ids := []string{}
	for i := 0; i < 2; i++ {
		done := make(chan error, 1)
		go func() { _, err := s.OnMessage(msg); done <- err }()
		select {
		case published := <-output.GetChannel():
			ids = append(ids, published.ID)
		case <-time.After(time.Second):
			t.Fatal("article was not published")
		}
		require.NoError(t, <-done)
	}
	require.Equal(t, ids[0], ids[1])
}
