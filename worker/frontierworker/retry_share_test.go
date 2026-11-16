package frontierworker

import (
	"math"
	"testing"
)

func TestRetrySchedulingHonorsConfiguredFractions(t *testing.T) {
	for _, share := range []float64{.01, .2, .21, .5, .9, 1} {
		retries := 0
		for step := uint64(0); step < 1000; step++ {
			if retryTurn(step, share) {
				retries++
			}
		}
		if math.Abs(float64(retries)-1000*share) > 1 {
			t.Fatalf("share %v scheduled %d retries", share, retries)
		}
	}
}
