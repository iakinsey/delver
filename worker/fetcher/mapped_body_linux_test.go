//go:build linux

package fetcher

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/resource/objectstore"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

type observedFileStore struct {
	objectstore.ObjectStore
	files   int
	spliced int64
}

func (s *observedFileStore) PutFile(ctx context.Context, key types.UUID, f *os.File) (string, int64, error) {
	s.files++
	digest, n, err := s.ObjectStore.(objectstore.FileObjectStore).PutFile(ctx, key, f)
	s.spliced += n
	return digest, n, err
}
func mappedFetcherFixture(t *testing.T) (*httpFetcher, *protection.Policy, *observedFileStore, *flow.Controller) {
	t.Helper()
	policy, err := protection.Open(t.TempDir() + "/policy.sqlite")
	require.NoError(t, err)
	t.Cleanup(func() { policy.Close() })
	controller, err := flow.New(context.Background(), string(types.NewV4()), flow.Default(), nil)
	require.NoError(t, err)
	t.Cleanup(controller.Close)
	store := &observedFileStore{ObjectStore: objectstore.NewFilesystemObjectStore(objectstore.FilesystemObjectStoreParams{Path: t.TempDir()})}
	worker := NewHttpFetcherWithConfig(HttpFetcherParams{ObjectStore: store}, config.Config{HTTPClient: config.HTTPClientConfig{Protection: policy, Flow: controller, Timeout: 3 * time.Second, MaxResponseBytes: 2 << 20, PerOriginConcurrency: 4, UserAgent: "MappedBodyFixture/1.0"}}).(*httpFetcher)
	t.Cleanup(worker.OnComplete)
	return worker, policy, store, controller
}
func mappedFetch(t *testing.T, worker *httpFetcher, ctx context.Context, uri string) (message.FetcherResponse, error) {
	t.Helper()
	raw, err := json.Marshal(message.FetcherRequest{URI: uri})
	require.NoError(t, err)
	out, err := worker.OnMessageContext(ctx, types.Message{Message: raw})
	if err != nil {
		return message.FetcherResponse{}, err
	}
	return out.(message.FetcherResponse), nil
}
func verifyMappedObject(t *testing.T, store objectstore.ObjectStore, response message.FetcherResponse, body []byte) {
	t.Helper()
	require.True(t, response.Success, response.Error)
	require.Equal(t, fmt.Sprintf("%x", md5.Sum(body)), response.ContentMD5)
	require.EqualValues(t, len(body), response.BytesReceived)
	require.EqualValues(t, len(body), response.BytesStored)
	file, err := store.Get(response.StoreKey)
	require.NoError(t, err)
	defer file.Close()
	got, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, body, got)
}
func TestMappedHTTPBodiesPreserveTransportAndFallback(t *testing.T) {
	for _, mode := range []string{"fixed", "gzip", "chunked", "redirect", "unavailable", "custom-store", "small"} {
		t.Run(mode, func(t *testing.T) {
			worker, _, store, controller := mappedFetcherFixture(t)
			body := bytes.Repeat([]byte("fixture payload "), 8192)
			if mode == "small" {
				body = []byte("small fixture")
			}
			var hits atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				require.Equal(t, "MappedBodyFixture/1.0", r.UserAgent())
				if r.URL.Path == "/redirect" {
					http.Redirect(w, r, "/body", 302)
					return
				}
				w.Header().Set("Content-Type", "text/html")
				switch mode {
				case "gzip":
					var compressed bytes.Buffer
					gz := gzip.NewWriter(&compressed)
					_, _ = gz.Write(body)
					require.NoError(t, gz.Close())
					w.Header().Set("Content-Encoding", "gzip")
					w.Header().Set("Content-Length", fmt.Sprint(compressed.Len()))
					_, _ = w.Write(compressed.Bytes())
				case "chunked":
					w.(http.Flusher).Flush()
					_, _ = w.Write(body)
				default:
					w.Header().Set("Content-Length", fmt.Sprint(len(body)))
					_, _ = w.Write(body)
				}
			}))
			defer server.Close()
			if mode == "unavailable" {
				worker.createMappedBody = func(int64) (*mappedBody, error) { return nil, unix.EPERM }
			}
			if mode == "custom-store" {
				worker.ObjectStore = struct{ objectstore.ObjectStore }{store.ObjectStore}
			}
			uri := server.URL + "/body"
			if mode == "redirect" {
				uri = server.URL + "/redirect"
			}
			response, err := mappedFetch(t, worker, context.Background(), uri)
			require.NoError(t, err)
			verifyMappedObject(t, worker.ObjectStore, response, body)
			count := int64(1)
			if mode == "redirect" {
				count = 2
			}
			require.Equal(t, count, hits.Load(), "fallback must not issue a second GET")
			mapped := mode == "fixed" || mode == "redirect"
			if mapped {
				require.Equal(t, 1, store.files)
				require.EqualValues(t, len(body), store.spliced)
			} else {
				require.Zero(t, store.files)
			}
			snapshot := controller.Snapshot()
			require.Zero(t, snapshot.NetworkActive)
			if mapped {
				require.EqualValues(t, len(body), snapshot.Counters["object_splice_bytes"])
			}
			if mode == "custom-store" {
				require.EqualValues(t, 1, snapshot.Counters["body_mapped"])
			}
			if mode == "unavailable" || mode == "gzip" || mode == "chunked" || mode == "small" {
				require.EqualValues(t, 1, snapshot.Counters["body_buffered"])
			}
		})
	}
}

func TestMappedDetectionStopsBeforeTheRemainderArrives(t *testing.T) {
	worker, policy, store, controller := mappedFetcherFixture(t)
	require.NoError(t, policy.Update(1, nil, []protection.Rule{{ID: "fixture", Enabled: true, Phrases: []string{"synthetic restricted marker"}}}))
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Length", fmt.Sprint(1<<20))
		_, _ = io.WriteString(w, "synthetic restricted marker")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(closed)
	}))
	defer server.Close()
	var allocated *mappedBody
	worker.createMappedBody = func(size int64) (*mappedBody, error) {
		var err error
		allocated, err = newMappedBody(size)
		return allocated, err
	}
	started := time.Now()
	response, err := mappedFetch(t, worker, context.Background(), server.URL)
	require.NoError(t, err)
	require.False(t, response.Success)
	require.Equal(t, "excluded", response.FailureClass)
	require.Empty(t, response.StoreKey)
	require.False(t, response.Retryable)
	require.Less(t, time.Since(started), time.Second, "inspection waited for the declared body length")
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("blocked connection remained open")
	}
	require.NotNil(t, allocated)
	require.Nil(t, allocated.data)
	_, err = allocated.file.Stat()
	require.ErrorIs(t, err, os.ErrClosed)
	require.Zero(t, store.files)
	require.Zero(t, controller.Snapshot().NetworkActive)
}

func TestMappedFailuresReleaseAnonymousFiles(t *testing.T) {
	for _, mode := range []string{"short", "cancel", "size"} {
		t.Run(mode, func(t *testing.T) {
			worker, _, store, controller := mappedFetcherFixture(t)
			if mode == "size" {
				worker.maxBytes = 64 << 10
			}
			sent := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Content-Length", fmt.Sprint(1<<20))
				_, _ = io.WriteString(w, "clean fixture")
				w.(http.Flusher).Flush()
				close(sent)
				if mode == "cancel" {
					<-r.Context().Done()
				}
				if mode == "size" {
					_, _ = io.WriteString(w, strings.Repeat("x", 1<<20))
				}
			}))
			defer server.Close()
			var allocated *mappedBody
			worker.createMappedBody = func(size int64) (*mappedBody, error) {
				var err error
				allocated, err = newMappedBody(size)
				return allocated, err
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancel" {
				go func() { <-sent; time.Sleep(20 * time.Millisecond); cancel() }()
			}
			response, err := mappedFetch(t, worker, ctx, server.URL)
			if mode == "cancel" {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.NoError(t, err)
				require.False(t, response.Success)
				require.Empty(t, response.StoreKey)
			}
			if mode != "size" {
				require.NotNil(t, allocated)
				require.Nil(t, allocated.data)
				_, err = allocated.file.Stat()
				require.ErrorIs(t, err, os.ErrClosed)
			}
			require.Zero(t, store.files)
			require.Zero(t, controller.Snapshot().NetworkActive)
		})
	}
}

type blockAtEOF struct {
	io.Reader
	block func()
	done  bool
}

func (r *blockAtEOF) Read(p []byte) (int, error) {
	n, e := r.Reader.Read(p)
	if e == io.EOF && !r.done {
		r.done = true
		r.block()
	}
	return n, e
}
func (r *blockAtEOF) Close() error { return nil }
func TestMappedFinalWriteGuardRejectsLateBlock(t *testing.T) {
	worker, policy, store, _ := mappedFetcherFixture(t)
	const uri = "http://late-block.test/page"
	body := bytes.Repeat([]byte("clean fixture "), 10000)
	source := &blockAtEOF{Reader: bytes.NewReader(body), block: func() {
		entries, err := protection.ParseList(uri)
		require.NoError(t, err)
		require.NoError(t, policy.Update(1, []protection.List{{ID: "late", Name: "late", Enabled: true, Updated: time.Now(), Refresh: "6h", Entries: entries}}, nil))
	}}
	worker.Client = &util.MockDelverHTTPClient{Response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, ContentLength: int64(len(body)), Body: source}}
	response, err := mappedFetch(t, worker, context.Background(), uri)
	require.NoError(t, err)
	require.False(t, response.Success)
	require.Equal(t, "excluded", response.FailureClass)
	require.Empty(t, response.StoreKey)
	require.Zero(t, store.files)
}

func TestMappedLargeResponsesAndLimitRecovery(t *testing.T) {
	for _, test := range []struct {
		name, contentType string
		size, limit       int
	}{
		{"binary-32MiB", "application/octet-stream", 32 << 20, 32 << 20},
		{"html-8MiB", "text/html", 8 << 20, 8 << 20},
		{"oversize", "application/octet-stream", 32 << 20, 1 << 20},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy, err := protection.Open(t.TempDir() + "/policy.sqlite")
			require.NoError(t, err)
			defer policy.Close()
			objects := t.TempDir()
			store := &observedFileStore{ObjectStore: objectstore.NewFilesystemObjectStore(objectstore.FilesystemObjectStoreParams{Path: objects})}
			worker := NewHttpFetcherWithConfig(HttpFetcherParams{ObjectStore: store}, config.Config{HTTPClient: config.HTTPClientConfig{Protection: policy, Timeout: 15 * time.Second, MaxResponseBytes: int64(test.limit)}}).(*httpFetcher)
			defer worker.OnComplete()
			body := bytes.Repeat([]byte("fixture payload "), test.size/16)
			small := body[:128<<10]
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				payload := body
				if r.URL.Path == "/small" {
					payload = small
				}
				w.Header().Set("Content-Type", test.contentType)
				w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
				_, _ = w.Write(payload)
			}))
			defer server.Close()
			response, err := mappedFetch(t, worker, context.Background(), server.URL)
			require.NoError(t, err)
			if test.size <= test.limit {
				verifyMappedObject(t, store, response, body)
				require.EqualValues(t, len(body), store.spliced)
			} else {
				require.False(t, response.Success)
				require.Empty(t, response.StoreKey)
				require.Contains(t, response.Error, "configured size limit")
				require.LessOrEqual(t, response.BytesReceived, int64(test.limit))
				require.Zero(t, store.files)
				entries, err := os.ReadDir(objects)
				require.NoError(t, err)
				require.Empty(t, entries)
			}
			response, err = mappedFetch(t, worker, context.Background(), server.URL+"/small")
			require.NoError(t, err)
			verifyMappedObject(t, store, response, small)
		})
	}
}
