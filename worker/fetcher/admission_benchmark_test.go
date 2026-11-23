package fetcher

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/resource/objectstore"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type admissionBenchmarkClient struct {
	body         []byte
	active, peak atomic.Int64
}
type admissionBenchmarkBody struct {
	*bytes.Reader
	owner   *admissionBenchmarkClient
	started bool
	done    sync.Once
}

func (b *admissionBenchmarkBody) Read(p []byte) (int, error) {
	if !b.started {
		b.started = true
		n := b.owner.active.Add(1)
		for old := b.owner.peak.Load(); n > old; old = b.owner.peak.Load() {
			if b.owner.peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	n, e := b.Reader.Read(p)
	if e == io.EOF {
		_ = b.Close()
	}
	return n, e
}
func (b *admissionBenchmarkBody) Close() error {
	b.done.Do(func() {
		if b.started {
			b.owner.active.Add(-1)
		}
	})
	return nil
}
func (c *admissionBenchmarkClient) Perform(string) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/octet-stream"}}, ContentLength: int64(len(c.body)), Body: &admissionBenchmarkBody{Reader: bytes.NewReader(c.body), owner: c}}, nil
}

func BenchmarkInspectionAdmission(b *testing.B) {
	p, err := protection.Open(b.TempDir() + "/policy.sqlite")
	if err != nil {
		b.Fatal(err)
	}
	defer p.Close()
	store := objectstore.NewFilesystemObjectStore(objectstore.FilesystemObjectStoreParams{Path: b.TempDir()})
	w := NewHttpFetcherWithConfig(HttpFetcherParams{ObjectStore: store}, config.Config{HTTPClient: config.HTTPClientConfig{Protection: p, MaxResponseBytes: 16 << 20}}).(*httpFetcher)
	defer w.OnComplete()
	c := &admissionBenchmarkClient{body: bytes.Repeat([]byte("x"), 64<<10)}
	w.Client = c
	raw, _ := json.Marshal(message.FetcherRequest{URI: "http://admission.invalid/page"})
	b.ReportAllocs()
	b.SetBytes(32 * int64(len(c.body)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for j := 0; j < 32; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				out, e := w.OnMessageContext(context.Background(), types.Message{Message: raw})
				if e != nil {
					b.Error(e)
					return
				}
				r := out.(message.FetcherResponse)
				if !r.Success {
					b.Error(r.Error)
					return
				}
				if e = store.Delete(r.StoreKey); e != nil {
					b.Error(e)
				}
			}()
		}
		wg.Wait()
	}
	b.StopTimer()
	b.ReportMetric(float64(c.peak.Load()), "peak-body-readers")
	if c.active.Load() != 0 {
		b.Fatal("active body leak")
	}
}
