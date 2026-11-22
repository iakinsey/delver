package gateway

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/iakinsey/delver/trap"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestLiveProtectionStructuralHashesIndexWithoutIntegerOverflow(t *testing.T) {
	address := strings.TrimSuffix(os.Getenv("DELVER_ES_URL"), "/")
	if address == "" {
		t.Skip("set DELVER_ES_URL to an isolated Elasticsearch instance")
	}
	index := "delver-hash-vector-" + uuid.NewString()
	gateway := NewSearchGatewayWithIndices([]string{address}, index, "").(*searchGateway)
	for _, definition := range types.Indices {
		if definition.Name == types.CompositeIndexable {
			definition.Name = index
			require.NoError(t, gateway.DeclareContext(context.Background(), definition))
		}
	}
	defer func() {
		request, e := http.NewRequest("DELETE", address+"/"+index, nil)
		require.NoError(t, e)
		response, e := http.DefaultClient.Do(request)
		require.NoError(t, e)
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		require.Less(t, response.StatusCode, 300)
	}()
	hashes := make(trap.HashVector, 64)
	for i := range hashes {
		hashes[i] = ^uint64(0) - uint64(i)
	}
	observation := trap.Observation{Signature: hashes, Targets: hashes[:8], Patterns: hashes[:12]}
	composite := message.CompositeAnalysis{FetcherResponse: message.FetcherResponse{FetcherRequest: message.FetcherRequest{URI: "http://harmless.test/"}}, Features: map[string]interface{}{features.AdversarialField: features.Adversarial{Structure: &observation}}}
	// Include the same generic JSON relay used by the durable worker pipeline.
	raw, e := json.Marshal(composite)
	require.NoError(t, e)
	require.NoError(t, json.Unmarshal(raw, &composite))
	require.NoError(t, gateway.IndexManyContext(context.Background(), []*types.Indexable{{ID: "hashes", Index: index, CrawlID: "synthetic", RunID: "run", Data: composite}}))
	response, e := http.Get(address + "/" + index + "/_doc/hashes")
	require.NoError(t, e)
	defer response.Body.Close()
	require.Equal(t, 200, response.StatusCode)
	var stored struct {
		Source message.CompositeAnalysis `json:"_source"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&stored))
	var actual features.Adversarial
	require.NoError(t, stored.Source.Load(features.AdversarialField, &actual))
	require.Equal(t, observation, *actual.Structure)
}
