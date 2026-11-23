package fetcher

import (
	"bytes"
	"context"
	"github.com/iakinsey/delver/util"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
	"io"
	"net/http"
	"testing"
)

type reservationObserver struct {
	io.Reader
	observe func()
}

func (r reservationObserver) Read(p []byte) (int, error) { r.observe(); return r.Reader.Read(p) }
func (r reservationObserver) Close() error               { return nil }
func TestMappedReservationAndFallbackStayWithinBudget(t *testing.T) {
	for _, mode := range []string{"mapped", "unavailable", "unknown", "misleading", "decompressed"} {
		t.Run(mode, func(t *testing.T) {
			w, p, _, _ := mappedFetcherFixture(t)
			w.maxBytes = 16 << 20
			body := bytes.Repeat([]byte("fixture "), 16384)
			length := int64(len(body))
			want := int64(2*len(body) + (8 << 20))
			if mode == "unavailable" {
				w.createMappedBody = func(int64) (*mappedBody, error) { return nil, unix.EPERM }
			}
			if mode == "unknown" {
				length = -1
			}
			if mode == "unknown" || mode == "unavailable" || mode == "decompressed" {
				want = 2*w.maxBytes + (64 << 10)
			}
			if mode == "misleading" {
				length--
			}
			observed := int64(0)
			source := reservationObserver{Reader: bytes.NewReader(body), observe: func() { observed = max(observed, p.MemorySnapshot().ReservedBytes) }}
			w.Client = &util.MockDelverHTTPClient{Response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Uncompressed: mode == "decompressed", ContentLength: length, Body: source}}
			r, err := mappedFetch(t, w, context.Background(), "http://fixture.invalid/")
			require.NoError(t, err)
			if mode == "misleading" {
				require.False(t, r.Success)
				require.Empty(t, r.StoreKey)
			} else {
				require.True(t, r.Success, r.Error)
				require.Equal(t, want, observed)
			}
			require.Zero(t, p.MemorySnapshot().Active)
			require.Zero(t, p.MemorySnapshot().ReservedBytes)
		})
	}
}
