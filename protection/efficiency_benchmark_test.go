package protection

import (
	"context"
	"fmt"
	"golang.org/x/sys/unix"
	"runtime"
	"strings"
	"testing"
)

// Synthetic inputs deliberately include markup and non-ASCII content; plain
// text alone overstates the benefit of avoiding a no-op tag replacement.
func BenchmarkProtectionEfficiency(b *testing.B) {
	for _, fixture := range []struct {
		name, text string
		fragment   int
	}{
		{"plain", "safe fixture words ", 32768},
		{"html", `<article><h1>Fixture</h1><p>A &amp; B</p></article>`, 32768},
		{"unicode", "界 É e\u0301 ＳＡＦＥ\u00a0fixture\u2003words ", 32768},
		{"malformed", `<div title='&amp;'>A <b>fixture</div>&#32;<unfinished`, 32768},
		{"json", `{"title":"safe fixture","items":[1,2,3]} `, 32768},
		{"fragmented", "safe fixture words ", 1024},
	} {
		b.Run(fixture.name, func(b *testing.B) {
			p, err := Open(b.TempDir() + "/policy.sqlite")
			if err != nil {
				b.Fatal(err)
			}
			defer p.Close()
			body := []byte(strings.Repeat(fixture.text, (128<<10)/len(fixture.text)))
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			var before, after unix.Rusage
			var initial, final runtime.MemStats
			runtime.ReadMemStats(&initial)
			_ = unix.Getrusage(unix.RUSAGE_SELF, &before)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r := p.InspectReader(context.Background(), nil, "http://fixture.invalid/page?q=fixture", "crawl", "run", 0)
				for start := 0; start < len(body); start += fixture.fragment {
					if err := r.InspectChunk(body[start:min(start+fixture.fragment, len(body))]); err != nil {
						b.Fatal(fmt.Errorf("synthetic inspection: %w", err))
					}
				}
				_ = r.Close()
			}
			b.StopTimer()
			_ = unix.Getrusage(unix.RUSAGE_SELF, &after)
			runtime.ReadMemStats(&final)
			b.ReportMetric(float64(after.Utime.Nano()+after.Stime.Nano()-before.Utime.Nano()-before.Stime.Nano())/float64(b.N), "cpu-ns/op")
			b.ReportMetric(float64(final.NumGC-initial.NumGC), "gc-cycles")
			b.ReportMetric(float64(after.Maxrss), "peak-rss-KiB")
		})
	}
}

func BenchmarkProtectedResponseSizes(b *testing.B) {
	for _, size := range []int{8 << 10, 121 << 10, 202 << 10, 1 << 20, 15 << 20} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			p, e := Open(b.TempDir() + "/policy.sqlite")
			if e != nil {
				b.Fatal(e)
			}
			defer p.Close()
			unit := "ordinary fixture content "
			body := []byte(strings.Repeat(unit, (size+len(unit)-1)/len(unit)))[:size]
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			var before, after unix.Rusage
			unix.Getrusage(unix.RUSAGE_SELF, &before)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r := p.InspectReader(context.Background(), nil, "http://fixture.invalid/page", "fixture", "run", 0)
				for at := 0; at < len(body); at += 32768 {
					if e = r.InspectChunk(body[at:min(at+32768, len(body))]); e != nil {
						b.Fatal(e)
					}
				}
				r.Close()
			}
			b.StopTimer()
			unix.Getrusage(unix.RUSAGE_SELF, &after)
			b.ReportMetric(float64(after.Utime.Nano()+after.Stime.Nano()-before.Utime.Nano()-before.Stime.Nano())/float64(b.N), "cpu-ns/op")
		})
	}
}
