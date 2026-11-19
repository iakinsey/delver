package frontier

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/iakinsey/delver/types/message"
)

// Run with -benchtime=1x -count=5 on persistent storage for before/after evidence.
func BenchmarkDiscoveryExtensionBatch(b *testing.B) {
	for _, excludedEvery := range []int{1, 2, 0} {
		name := map[int]string{1: "excluded", 2: "mixed", 0: "allowed"}[excludedEvery]
		b.Run(name, func(b *testing.B) {
			var elapsed time.Duration
			var commits, stageCommits uint64
			for iteration := 0; iteration < b.N; iteration++ {
				s, err := OpenStore(StoreParams{Path: filepath.Join(b.TempDir(), "db"), ExcludedExtensions: []string{"pdf"}})
				if err != nil {
					b.Fatal(err)
				}
				requests := make([]message.FetcherRequest, 2048)
				for i := range requests {
					suffix := ".html"
					if excludedEvery > 0 && i%excludedEvery == 0 {
						suffix = ".pdf"
					}
					requests[i].URI = fmt.Sprintf("http://fixture.test/%d%s", i, suffix)
				}
				version := admissionVersion(s)
				started := time.Now()
				if err := s.StageDiscovery(context.Background(), "batch", requests); err != nil {
					b.Fatal(err)
				}
				stageCommits += admissionVersion(s) - version
				for {
					n, err := s.DrainDiscovery(context.Background(), 256)
					if err != nil {
						b.Fatal(err)
					}
					if n == 0 {
						break
					}
				}
				elapsed += time.Since(started)
				commits += admissionVersion(s) - version
				counts, err := s.Counts()
				if err != nil {
					b.Fatal(err)
				}
				want := int64(0)
				if excludedEvery > 0 {
					want = int64(len(requests) / excludedEvery)
				}
				if counts["policy_skips"] != want || counts["admitted"] != int64(len(requests))-want || counts["deferred_discoveries"] != 0 {
					b.Fatal(counts)
				}
				if err := s.Close(); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(commits)/float64(b.N), "commits/batch")
			b.ReportMetric(float64(stageCommits)/float64(b.N), "stage_commits/batch")
			b.ReportMetric(float64(elapsed.Microseconds())/1000/float64(b.N), "discovery_ms/batch")
			b.ReportMetric(float64(2048*b.N)/elapsed.Seconds(), "links/s")
		})
	}
}
