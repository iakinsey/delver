package frontier

import (
	"context"
	"fmt"
	"github.com/iakinsey/delver/types/message"
	"strings"
	"testing"
)

func TestLargeStagedRecordsBatchFallbackReplayAndRestart(t *testing.T) {
	ctx := context.Background()
	p := StoreParams{Path: t.TempDir(), MaxPending: 512}
	s, e := OpenStore(p)
	if e != nil {
		t.Fatal(e)
	}
	rs := make([]message.FetcherRequest, 128)
	for i := range rs {
		rs[i] = message.FetcherRequest{URI: fmt.Sprintf("http://fixture.invalid/%s/%d", strings.Repeat("a", 16000), i)}
	}
	if e = s.StageDiscovery(ctx, "large", rs); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DrainDiscovery(ctx, 128); e != nil {
		t.Fatal(e)
	}
	if e = s.StageDiscovery(ctx, "large", rs); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = OpenStore(p)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	counts, e := s.Counts()
	if e != nil || counts["admitted"] != 128 || counts["deferred_discoveries"] != 0 {
		t.Fatal(counts, e)
	}
}
