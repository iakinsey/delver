package report

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func BenchmarkReportResponseWrites(b *testing.B) {
	for _, workers := range []int{1, 32} {
		b.Run(fmt.Sprint(workers), func(b *testing.B) {
			for iteration := 0; iteration < b.N; iteration++ {
				b.StopTimer()
				s, err := Open(filepath.Join(b.TempDir(), "report.db"), false)
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				var wg sync.WaitGroup
				for w := 0; w < workers; w++ {
					wg.Add(1)
					go func(w int) {
						defer wg.Done()
						for n := 0; n < 64; n++ {
							if e := s.Response(context.Background(), fmt.Sprintf("http://fixture.invalid/%d/%d", w, n), 200); e != nil {
								b.Error(e)
								return
							}
						}
					}(w)
				}
				wg.Wait()
				b.StopTimer()
				counts, err := s.Counts(context.Background())
				if err != nil || counts.Visited != int64(64*workers) {
					b.Fatal(counts, err)
				}
				if err = s.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
