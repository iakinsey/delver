package frontier

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/iakinsey/delver/types/message"
	"github.com/stretchr/testify/require"
)

func exclusionBatch(n int, mixed bool) []message.FetcherRequest {
	requests := make([]message.FetcherRequest, n)
	for i := range requests {
		suffix := ".PDF"
		if mixed && i%2 != 0 {
			suffix = ".html"
		}
		requests[i].URI = fmt.Sprintf("http://fixture.test/%d%s?download=1#fragment", i, suffix)
	}
	return requests
}

func TestDiscoveryExclusionsNeverStageAndCountOncePerChunk(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprint(mixed), func(t *testing.T) {
			s := admissionStore(t, StoreParams{ExcludedExtensions: []string{"pdf"}})
			requests := exclusionBatch(2048, mixed)
			version := admissionVersion(s)
			require.NoError(t, s.StageDiscovery(context.Background(), "batch", requests))
			require.EqualValues(t, 16, admissionVersion(s)-version, "one commit per original 128-link chunk")
			counts, err := s.Counts()
			require.NoError(t, err)
			want := int64(2048)
			if mixed {
				want = 1024
			}
			require.Equal(t, want, counts["policy_skips"])
			require.Equal(t, int64(2048)-want, counts["deferred_discoveries"])
			for i := 0; i < 3; i++ {
				require.NoError(t, s.StageDiscovery(context.Background(), "batch", requests))
			}
			for {
				n, err := s.DrainDiscovery(context.Background(), 256)
				require.NoError(t, err)
				if n == 0 {
					break
				}
			}
			require.NoError(t, s.StageDiscovery(context.Background(), "batch", requests))
			counts, err = s.Counts()
			require.NoError(t, err)
			require.Equal(t, want, counts["policy_skips"])
			require.Equal(t, int64(2048)-want, counts["admitted"])
			require.Zero(t, counts["deferred_discoveries"])
			if mixed {
				rows, _, err := s.Pending(context.Background(), "", 2048)
				require.NoError(t, err)
				require.Len(t, rows, 1024)
				for _, r := range rows {
					require.True(t, strings.HasSuffix(r.Request.URI, ".html?download=1"))
				}
			}
		})
	}
}

type cancelAfterDiscoveryCommit struct {
	context.Context
	store   *Store
	version uint64
}

func (c cancelAfterDiscoveryCommit) Err() error {
	if admissionVersion(c.store) > c.version {
		return context.Canceled
	}
	return nil
}

func TestDiscoveryExcludedChunkCancellationRestartAndPolicyChange(t *testing.T) {
	p := StoreParams{Path: filepath.Join(t.TempDir(), "db"), ExcludedExtensions: []string{"pdf"}}
	s, err := OpenStore(p)
	require.NoError(t, err)
	requests := exclusionBatch(300, true)
	ctx := cancelAfterDiscoveryCommit{Context: context.Background(), store: s, version: admissionVersion(s)}
	require.ErrorIs(t, s.StageDiscovery(ctx, "interrupted", requests), context.Canceled)
	counts, err := s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 64, counts["policy_skips"])
	require.EqualValues(t, 64, counts["deferred_discoveries"])
	require.NoError(t, s.Close())
	s, err = OpenStore(p)
	require.NoError(t, err)
	require.NoError(t, s.StageDiscovery(context.Background(), "interrupted", requests))
	for {
		n, err := s.DrainDiscovery(context.Background(), 64)
		require.NoError(t, err)
		if n == 0 {
			break
		}
	}
	require.NoError(t, s.Close())
	p.ExcludedExtensions = nil
	s, err = OpenStore(p)
	require.NoError(t, err)
	defer s.Close()
	// Replaying the committed message retains its original decision and count.
	require.NoError(t, s.StageDiscovery(context.Background(), "interrupted", requests))
	counts, err = s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 150, counts["policy_skips"])
	require.EqualValues(t, 150, counts["admitted"])
	require.Zero(t, counts["deferred_discoveries"])
	// A new discovery after removing the rule can admit previously excluded URLs.
	require.NoError(t, s.StageDiscovery(context.Background(), "rediscovery", requests))
	for {
		n, err := s.DrainDiscovery(context.Background(), 64)
		require.NoError(t, err)
		if n == 0 {
			break
		}
	}
	counts, err = s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 300, counts["admitted"])
	require.EqualValues(t, 150, counts["policy_skips"])
}

func TestDiscoveryConcurrentReplayKeepsExactExcludedCounts(t *testing.T) {
	s := admissionStore(t, StoreParams{ExcludedExtensions: []string{"pdf"}})
	var wg sync.WaitGroup
	errors := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errors <- s.StageDiscovery(context.Background(), "same-message", exclusionBatch(1024, true))
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 8; j++ {
				_, err := s.DrainDiscovery(context.Background(), 128)
				if err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	counts, err := s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 512, counts["policy_skips"])
	require.EqualValues(t, 512, counts["admitted"])
	require.Zero(t, counts["deferred_discoveries"])
}

func TestDiscoveryExistingStagedExclusionsAcknowledgeInBatches(t *testing.T) {
	p := StoreParams{Path: filepath.Join(t.TempDir(), "db")}
	s, err := OpenStore(p)
	require.NoError(t, err)
	require.NoError(t, s.StageDiscovery(context.Background(), "old-message", exclusionBatch(1024, false)))
	require.NoError(t, s.Close())
	p.ExcludedExtensions = []string{"pdf"}
	s, err = OpenStore(p)
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close()) }()
	version := admissionVersion(s)
	// Interrupt after a committed acknowledgment chunk, then replay from disk.
	ctx := cancelAfterDiscoveryCommit{Context: context.Background(), store: s, version: version}
	_, err = s.DrainDiscovery(ctx, 1024)
	require.ErrorIs(t, err, context.Canceled)
	counts, err := s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 128, counts["policy_skips"])
	require.EqualValues(t, 896, counts["deferred_discoveries"])
	require.NoError(t, s.Close())
	s, err = OpenStore(p)
	require.NoError(t, err)
	version = admissionVersion(s)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.DrainDiscovery(context.Background(), 1024)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	require.LessOrEqual(t, admissionVersion(s)-version, uint64(8), "old staged exclusions wrote per record")
	counts, err = s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 1024, counts["policy_skips"])
	require.Zero(t, counts["deferred_discoveries"])
	require.Zero(t, counts["admitted"])
	require.NoError(t, s.StageDiscovery(context.Background(), "old-message", exclusionBatch(1024, false)))
	counts, err = s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 1024, counts["policy_skips"])
}

func TestDiscoveryExclusionInputsAndAtomicFailure(t *testing.T) {
	for _, input := range []string{".zip,.pdf, .docx", "zip, pdf, docx", "zip, tar.gz,    pdf", ".zip, .tar.gz, pdf,tar.xz"} {
		t.Run(input, func(t *testing.T) {
			s := admissionStore(t, StoreParams{ExcludedExtensions: strings.Split(input, ",")})
			requests := []message.FetcherRequest{{URI: "https://fixture.test/archive.ZIP?q=1"}, {URI: "https://fixture.test/document.%70df"}, {URI: "https://fixture.test/page.html?file=archive.zip"}}
			require.NoError(t, s.StageDiscovery(context.Background(), "formats", requests))
			counts, err := s.Counts()
			require.NoError(t, err)
			require.EqualValues(t, 2, counts["policy_skips"])
			require.EqualValues(t, 1, counts["deferred_discoveries"])
		})
	}
	s := admissionStore(t, StoreParams{ExcludedExtensions: []string{"pdf"}})
	requests := []message.FetcherRequest{{URI: "http://fixture.test/allowed"}, {URI: "http://fixture.test/skipped.pdf"}, {URI: "http://fixture.test/invalid.pdf", Depth: -1}}
	require.Error(t, s.StageDiscovery(context.Background(), "abort", requests))
	counts, err := s.Counts()
	require.NoError(t, err)
	require.Zero(t, counts["policy_skips"])
	require.Zero(t, counts["deferred_discoveries"])
	requests[2].Depth = 0
	require.NoError(t, s.StageDiscovery(context.Background(), "abort", requests))
	counts, err = s.Counts()
	require.NoError(t, err)
	require.EqualValues(t, 2, counts["policy_skips"])
	require.EqualValues(t, 1, counts["deferred_discoveries"])
}
