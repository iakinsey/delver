package frontier

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/trap"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
)

func TestGatewayThrottlePersistsAndKeepsRepresentatives(t *testing.T) {
	ctx := context.Background()
	settings := trap.Default()
	params := StoreParams{Path: filepath.Join(t.TempDir(), "frontier"), Traps: &settings, RevisitAfter: 1, OriginDelay: 1}
	s, err := OpenStore(params)
	require.NoError(t, err)
	signature := make(trap.HashVector, 64)
	for i := range signature {
		signature[i] = ^uint64(i)
	}
	observe := func(host, target string) bool {
		uri := "http://" + host + "/"
		c := message.CompositeAnalysis{
			FetcherResponse: message.FetcherResponse{
				FetcherRequest: message.FetcherRequest{URI: uri, ScopeRoot: uri},
				Success:        true, HTTPCode: 200,
			},
			Features: map[string]interface{}{features.AdversarialField: features.Adversarial{
				Structure: &trap.Observation{Site: host, URI: uri, Signature: signature, Tokens: 40},
			}},
		}
		blocked, err := s.ObserveGateway(ctx, c, features.URIs{target})
		require.NoError(t, err)
		return blocked
	}
	target := "https://destination.test/"
	pending := "http://gateway-5.test/revisit"
	require.NoError(t, s.Admit(ctx, message.FetcherRequest{URI: pending}))
	page, err := s.ScanCandidates(ctx, "breadth_first", ScanCursor{}, 32)
	require.NoError(t, err)
	require.Len(t, page.Candidates, 1)
	_, err = s.Reserve(ctx, page.Candidates[0])
	require.NoError(t, err)
	for i := 0; i < 7; i++ {
		host := fmt.Sprintf("gateway-%d.test", i)
		require.Equal(t, i >= 5, observe(host, target))
		require.Equal(t, i >= 5, observe(host, "https://DESTINATION.test:443/#section"))
	}
	require.False(t, observe("other-target.test", "https://different.test/"))
	signature[0] = 42
	require.False(t, observe("other-template.test", target))
	signature[0] = ^uint64(0)
	require.NoError(t, s.Close())
	s, err = OpenStore(params)
	require.NoError(t, err)
	defer s.Close()
	decisions, err := s.TrapDecisions(ctx)
	require.NoError(t, err)
	require.Len(t, decisions, 2)
	require.Equal(t, gatewayReason, decisions[0].Reason)
	require.Equal(t, target, decisions[0].Gateway.Destination)
	require.Equal(t, 7, decisions[0].Gateway.Members)
	require.Equal(t, 2, decisions[0].Gateway.Suppressed)
	require.Len(t, decisions[0].Gateway.Representatives, 5)
	require.NoError(t, s.db.View(func(tx *badger.Txn) error {
		require.True(t, s.trapBlocked(tx, "http://gateway-5.test/revisit"))
		require.False(t, s.trapBlocked(tx, "http://gateway-0.test/"))
		require.False(t, s.trapBlocked(tx, target))
		return nil
	}))
	page, err = s.ScanCandidates(ctx, "breadth_first", ScanCursor{}, 32)
	require.NoError(t, err)
	require.Empty(t, page.Candidates)
	require.Len(t, page.Trapped, 1)
	require.NoError(t, s.ParkTraps(ctx, page.Trapped))
	require.NoError(t, s.RestoreTrap(ctx, "gateway-5.test"))
	require.NoError(t, s.DrainParked(ctx, 32))
	page, err = s.ScanCandidates(ctx, "breadth_first", ScanCursor{}, 32)
	require.NoError(t, err)
	require.Len(t, page.Candidates, 1)
	require.Equal(t, pending, page.Candidates[0].Request.URI)
	require.False(t, observe("gateway-5.test", target))
	require.NoError(t, s.db.View(func(tx *badger.Txn) error {
		require.False(t, s.trapBlocked(tx, "http://gateway-5.test/revisit"))
		return nil
	}))
}
