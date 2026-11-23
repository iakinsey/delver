//go:build !race

package protection

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

// Allocation-volume measurement for production builds. The race runtime
// deliberately changes sync.Pool reuse and allocation behavior; reservation
// correctness is independently tested under -race in memory_metrics_test.go
// and worker/fetcher/reservation_test.go.
//
// Size-aware admission allows 8 MiB beyond twice the mapped response length.
// Exercise the highest-expansion normalization inputs one bounded chunk at a
// time. Total allocated bytes bound all simultaneously live temporaries.
func TestInspectionTransientAllowance(t *testing.T) {
	p, e := Open(t.TempDir() + "/policy.db")
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	for _, unit := range []string{"\ufdfa", "\u0130\ufb03", "&CounterClockwiseContourIntegral;", "<span data-x='fixture'>x</span>", "\xff"} {
		body := []byte(strings.Repeat(unit, (32<<10)/len(unit)))
		inspector := p.InspectReader(context.Background(), nil, "http://fixture.invalid/", "fixture", "run", 0)
		for i := 0; i < 4; i++ {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			if e = inspector.InspectChunk(body); e != nil {
				t.Fatal(e)
			}
			runtime.ReadMemStats(&after)
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8<<20 {
				t.Fatalf("%q chunk allocated %d bytes, exceeds conservative allowance", unit, allocated)
			}
		}
		inspector.Close()
	}
}
