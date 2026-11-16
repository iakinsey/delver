package frontier

import (
	"context"
	"fmt"
	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/types/message"
	"path/filepath"
	"testing"
)

func BenchmarkFrontierSeek(b *testing.B) {
	for _, history := range []int{0, 1000, 10000} {
		b.Run(fmt.Sprint(history), func(b *testing.B) {
			s, err := OpenStore(StoreParams{Path: filepath.Join(b.TempDir(), "db")})
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			if err := s.db.Update(func(tx *badger.Txn) error {
				for i := 0; i < history; i++ {
					uri := fmt.Sprintf("https://history.test/%d", i)
					if err := write(tx, "u/"+uri, Candidate{Request: message.FetcherRequest{URI: uri}, State: "dedup", Terminal: 1}); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				b.Fatal(err)
			}
			for i := 0; i < 100; i++ {
				if err := s.Admit(context.Background(), message.FetcherRequest{URI: fmt.Sprintf("https://h%d.test/", i)}); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cs, err := s.Candidates(context.Background(), "host_diverse", "", 32)
				if err != nil || len(cs) != 32 {
					b.Fatal(len(cs), err)
				}
			}
		})
	}
}
