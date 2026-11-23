package frontier

import (
	"context"
	"fmt"
	"github.com/iakinsey/delver/types/message"
	"golang.org/x/sys/unix"
	"testing"
)

func BenchmarkStagedDeduplication(b *testing.B) {
	for _, unique := range []int{256, 2048, 4096} {
		b.Run(fmt.Sprint(unique), func(b *testing.B) {
			var cpu int64
			for iteration := 0; iteration < b.N; iteration++ {
				b.StopTimer()
				s, e := OpenStore(StoreParams{Path: b.TempDir(), MaxPending: 10000})
				if e != nil {
					b.Fatal(e)
				}
				requests := make([]message.FetcherRequest, 4096)
				for i := range requests {
					requests[i] = message.FetcherRequest{URI: fmt.Sprintf("http://fixture.invalid/page/%d", i%unique), Depth: 2, Hops: 1}
				}
				version := admissionVersion(s)
				cpuStart := frontierBenchmarkCPU()
				b.StartTimer()
				for start := 0; start < len(requests); start += 128 {
					if e = s.StageDiscovery(context.Background(), fmt.Sprintf("event-%d", start), requests[start:start+128]); e != nil {
						b.Fatal(e)
					}
				}
				staged, _ := s.Counts()
				for pass := 0; pass < 100; pass++ {
					counts, _ := s.Counts()
					if counts["deferred_discoveries"] == 0 {
						break
					}
					if _, e = s.DrainDiscovery(context.Background(), 256); e != nil {
						b.Fatal(e)
					}
				}
				counts, e := s.Counts()
				if e != nil || counts["admitted"] != int64(unique) || counts["deferred_discoveries"] != 0 {
					b.Fatal(counts, e)
				}
				b.ReportMetric(float64(admissionVersion(s)-version), "commits/op")
				if e = s.Close(); e != nil {
					b.Fatal(e)
				}
				b.StopTimer()
				cpu += frontierBenchmarkCPU() - cpuStart
				b.ReportMetric(float64(staged["deferred_discoveries"]), "staged-records")
				b.ReportMetric(float64(unique), "unique-urls")
			}
			b.ReportMetric(float64(cpu)/float64(b.N), "cpu-ns/op")
		})
	}
}

func BenchmarkStagingTransfer(b *testing.B) {
	for _, unique := range []int{256, 4096} {
		b.Run(fmt.Sprint(unique), func(b *testing.B) {
			var cpu int64
			for iteration := 0; iteration < b.N; iteration++ {
				b.StopTimer()
				s, e := OpenStore(StoreParams{Path: b.TempDir()})
				if e != nil {
					b.Fatal(e)
				}
				rs := make([]message.FetcherRequest, 4096)
				for i := range rs {
					rs[i] = message.FetcherRequest{URI: fmt.Sprintf("http://fixture.invalid/%d", i%unique), Depth: 2, Hops: 1}
				}
				version := admissionVersion(s)
				cpuStart := frontierBenchmarkCPU()
				b.StartTimer()
				for start := 0; start < len(rs); start += 128 {
					if e = s.StageDiscovery(context.Background(), fmt.Sprint(start), rs[start:start+128]); e != nil {
						b.Fatal(e)
					}
				}
				b.StopTimer()
				cpu += frontierBenchmarkCPU() - cpuStart
				b.ReportMetric(float64(admissionVersion(s)-version), "commits/op")
				counts, e := s.Counts()
				if e != nil {
					b.Fatal(e)
				}
				b.ReportMetric(float64(counts["deferred_discoveries"]), "staged-records")
				if e = s.Close(); e != nil {
					b.Fatal(e)
				}
			}
			b.ReportMetric(float64(cpu)/float64(b.N), "cpu-ns/op")
		})
	}
}

func frontierBenchmarkCPU() int64 {
	var r unix.Rusage
	_ = unix.Getrusage(unix.RUSAGE_SELF, &r)
	return r.Utime.Nano() + r.Stime.Nano()
}
