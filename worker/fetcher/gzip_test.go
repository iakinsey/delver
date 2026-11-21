package fetcher

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/resource/objectstore"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/stretchr/testify/require"
)

type diskFullObjectStore struct{ objectstore.ObjectStore }

func (diskFullObjectStore) Put(types.UUID, io.Reader) (string, error) {
	return "", syscall.ENOSPC
}

func TestCompressedResponseFailuresDoNotReportLocalOverload(t *testing.T) {
	const content = "<html><body>compressed response</body></html>"
	var encoded bytes.Buffer
	w := gzip.NewWriter(&encoded)
	_, err := io.WriteString(w, content)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	valid := encoded.Bytes()
	checksum := bytes.Clone(valid)
	checksum[len(checksum)-8] ^= 1
	deflate := append(bytes.Clone(valid[:10]), 0x07) // Reserved DEFLATE block type.
	deflate = append(deflate, make([]byte, 8)...)

	for _, tc := range []struct {
		name, kind, errorText string
		payload               []byte
		diskFull              bool
	}{
		{name: "valid", payload: valid},
		{name: "checksum", payload: checksum, kind: "body", errorText: "gzip: invalid checksum"},
		{name: "header", payload: []byte("invalid gzip response"), kind: "body", errorText: "gzip: invalid header"},
		{name: "deflate", payload: deflate, kind: "body", errorText: "flate: corrupt input"},
		{name: "truncated", payload: valid[:len(valid)-4], kind: "body", errorText: "unexpected EOF"},
		{name: "disk full", payload: valid, diskFull: true, kind: "storage", errorText: "no space left on device"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write(tc.payload)
			}))
			defer server.Close()
			policy := flow.Default()
			policy.Mode = "adaptive"
			policy.Concurrency = 4
			policy.MinSamples = 1
			controller, err := flow.New(context.Background(), t.Name(), policy, nil)
			require.NoError(t, err)
			defer controller.Close()
			path := t.TempDir()
			store := objectstore.NewFilesystemObjectStore(objectstore.FilesystemObjectStoreParams{Path: path})
			if tc.diskFull {
				store = diskFullObjectStore{store}
			}
			fetcher := &httpFetcher{
				flow: controller, ObjectStore: store,
				Client: util.NewHTTPClientWithConfig(config.HTTPClientConfig{Timeout: 2 * time.Second, Flow: controller}),
			}
			defer fetcher.OnComplete()
			raw, err := json.Marshal(message.FetcherRequest{RequestID: types.NewV4(), URI: server.URL})
			require.NoError(t, err)
			result, err := fetcher.OnMessage(types.Message{Message: raw})
			require.NoError(t, err)
			response := result.(message.FetcherResponse)
			require.Equal(t, tc.kind, response.FailureClass)
			if tc.kind == "" {
				require.True(t, response.Success)
				file, err := store.Get(response.StoreKey)
				require.NoError(t, err)
				defer file.Close()
				body, err := io.ReadAll(file)
				require.NoError(t, err)
				require.Equal(t, content, string(body))
				require.EqualValues(t, len(content), response.BytesStored)
			} else {
				require.False(t, response.Success)
				require.Contains(t, response.Error, tc.errorText)
				require.Equal(t, tc.kind == "body", response.Retryable)
				require.Empty(t, response.StoreKey)
				require.Zero(t, response.BytesStored)
				entries, err := os.ReadDir(path)
				require.NoError(t, err)
				require.Empty(t, entries, "failed responses must not leave partial objects")
			}
			snapshot := controller.Snapshot()
			require.EqualValues(t, 1, snapshot.Totals.Attempts)
			if tc.diskFull {
				require.EqualValues(t, 1, snapshot.Totals.Overload)
			} else {
				require.Zero(t, snapshot.Totals.Overload)
				controller.Evaluate(time.Now().Add(time.Minute), flow.Pressure{FetchPending: 10})
				require.Equal(t, 4, controller.Effective(), "remote body corruption must not reduce concurrency")
			}
		})
	}
}
