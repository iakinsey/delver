//go:build linux

package fetcher

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/resource/objectstore"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"golang.org/x/sys/unix"
)

// This measures the actual fetch worker and ordinary HTTP transport, including
// incremental protection, checksum, atomic publication and response cleanup.
func BenchmarkProtectedFetchBody(b *testing.B) {
	for _, size := range []int{8 << 10, 121 << 10, 202 << 10, 1 << 20, 15 << 20} {
		b.Run(fmt.Sprintf("%dKiB", size>>10), func(b *testing.B) {
			body := bytes.Repeat([]byte("fixture payload "), size/16)
			expected := fmt.Sprintf("%x", md5.Sum(body))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Content-Length", fmt.Sprint(len(body)))
				_, _ = w.Write(body)
			}))
			defer server.Close()
			policy, err := protection.Open(b.TempDir() + "/policy.sqlite")
			if err != nil {
				b.Fatal(err)
			}
			defer policy.Close()
			store := objectstore.NewFilesystemObjectStore(objectstore.FilesystemObjectStoreParams{Path: b.TempDir()})
			worker := NewHttpFetcherWithConfig(HttpFetcherParams{ObjectStore: store}, config.Config{HTTPClient: config.HTTPClientConfig{Protection: policy, PerOriginConcurrency: 8, MaxResponseBytes: 16 << 20}}).(*httpFetcher)
			defer worker.OnComplete()
			raw, _ := json.Marshal(message.FetcherRequest{URI: server.URL})
			b.SetParallelism(2)
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			var before, after unix.Rusage
			_ = unix.Getrusage(unix.RUSAGE_SELF, &before)
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					out, err := worker.OnMessageContext(context.Background(), types.Message{Message: raw})
					if err != nil {
						b.Error(err)
						return
					}
					response := out.(message.FetcherResponse)
					if !response.Success || response.ContentMD5 != expected {
						b.Errorf("response integrity: success=%v error=%s", response.Success, response.Error)
						return
					}
					if err := store.Delete(response.StoreKey); err != nil {
						b.Error(err)
						return
					}
				}
			})
			b.StopTimer()
			_ = unix.Getrusage(unix.RUSAGE_SELF, &after)
			cpu := after.Utime.Nano() - before.Utime.Nano() + after.Stime.Nano() - before.Stime.Nano()
			b.ReportMetric(float64(cpu)/float64(b.N), "cpu-ns/op")
			b.ReportMetric(float64(after.Maxrss), "process-peak-rss-KiB")
		})
	}
}
