//go:build linux

package objectstore

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/iakinsey/delver/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// Run against a disposable small tmpfs, never a production storage directory.
func TestFilePublicationDiskFull(t *testing.T) {
	root := os.Getenv("DELVER_OBJECTSTORE_FULL_DIR")
	if root == "" {
		t.Skip("set DELVER_OBJECTSTORE_FULL_DIR to an isolated tmpfs of at most 4 MiB")
	}
	var stats unix.Statfs_t
	require.NoError(t, unix.Statfs(root, &stats))
	require.EqualValues(t, unix.TMPFS_MAGIC, stats.Type)
	require.LessOrEqual(t, stats.Blocks*uint64(stats.Bsize), uint64(4<<20))
	path, err := os.MkdirTemp(root, "delver-full-")
	require.NoError(t, err)
	defer os.RemoveAll(path)
	source, err := os.CreateTemp(t.TempDir(), "source")
	require.NoError(t, err)
	defer source.Close()
	require.NoError(t, source.Truncate(8<<20))
	store := NewFilesystemObjectStore(FilesystemObjectStoreParams{Path: path}).(FileObjectStore)
	key := types.NewV4()
	_, _, err = store.PutFile(context.Background(), key, source)
	require.ErrorIs(t, err, unix.ENOSPC)
	entries, err := os.ReadDir(path)
	require.NoError(t, err)
	require.Empty(t, entries, "failed publication must remove its temporary object")
	require.NoError(t, source.Truncate(1024))
	_, _, err = store.PutFile(context.Background(), key, source)
	require.NoError(t, err)
	stored, err := os.Stat(filepath.Join(path, string(key)))
	require.NoError(t, err)
	require.EqualValues(t, 1024, stored.Size())
}

func TestFilePublicationSplicesAndChecksums(t *testing.T) {
	body := bytes.Repeat([]byte("verified fixture payload\n"), 20000)
	source, err := os.CreateTemp(t.TempDir(), "source")
	require.NoError(t, err)
	defer source.Close()
	_, err = source.Write(body)
	require.NoError(t, err)
	store := NewFilesystemObjectStore(FilesystemObjectStoreParams{Path: t.TempDir()}).(FileObjectStore)
	key := types.NewV4()
	digest, spliced, err := store.PutFile(context.Background(), key, source)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%x", md5.Sum(body)), digest)
	require.EqualValues(t, len(body), spliced, "this Linux acceptance case requires actual splice support")
	file, err := store.Get(key)
	require.NoError(t, err)
	got, err := io.ReadAll(file)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.Equal(t, body, got)
	for _, test := range []struct {
		name   string
		ctx    context.Context
		key    types.UUID
		source *os.File
	}{
		{"invalid-key", context.Background(), "../invalid", source},
		{"cancelled", func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }(), types.NewV4(), source},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := store.PutFile(test.ctx, test.key, test.source)
			require.Error(t, err)
			entries, err := os.ReadDir(store.(*filesystemObjectStore).Path)
			require.NoError(t, err)
			require.Len(t, entries, 1)
		})
	}
}

func TestSpliceCopyFallbacksKeepEveryByte(t *testing.T) {
	for _, fault := range []string{"unsupported-input", "unsupported-output", "partial-output", "interrupted", "cancelled", "write-error"} {
		t.Run(fault, func(t *testing.T) {
			body := bytes.Repeat([]byte("not duplicated or skipped"), 5000)
			source, err := os.CreateTemp(t.TempDir(), "input")
			require.NoError(t, err)
			defer source.Close()
			_, err = source.Write(body)
			require.NoError(t, err)
			_, err = source.Seek(0, io.SeekStart)
			require.NoError(t, err)
			destination, err := os.CreateTemp(t.TempDir(), "output")
			require.NoError(t, err)
			defer destination.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			inputs, outputs := 0, 0
			call := func(in int, ioff *int64, out int, ooff *int64, n, flags int) (int64, error) {
				if in == int(source.Fd()) {
					inputs++
					if fault == "unsupported-input" {
						return -1, unix.ENOSYS
					}
					if fault == "interrupted" && inputs == 1 {
						return -1, unix.EINTR
					}
				} else {
					outputs++
					if fault == "unsupported-output" {
						return -1, unix.EINVAL
					}
					if fault == "partial-output" && outputs == 2 {
						return -1, unix.EOPNOTSUPP
					}
					if fault == "interrupted" && outputs == 1 {
						return -1, unix.EINTR
					}
					if fault == "write-error" {
						return -1, unix.ENOSPC
					}
					if fault == "partial-output" || fault == "cancelled" {
						n = min(n, 127)
					}
				}
				written, err := unix.Splice(in, ioff, out, ooff, n, flags)
				if fault == "cancelled" && in != int(source.Fd()) {
					cancel()
				}
				return written, err
			}
			spliced, err := copyFileWithSplice(ctx, destination, source, call)
			if fault == "cancelled" {
				require.ErrorIs(t, err, context.Canceled)
				require.EqualValues(t, 127, spliced)
				return
			}
			if fault == "write-error" {
				require.ErrorIs(t, err, unix.ENOSPC)
				require.Zero(t, spliced)
				return
			}
			require.NoError(t, err)
			_, err = destination.Seek(0, io.SeekStart)
			require.NoError(t, err)
			got, err := io.ReadAll(destination)
			require.NoError(t, err)
			require.Equal(t, body, got)
			if fault == "unsupported-input" || fault == "unsupported-output" {
				require.Zero(t, spliced)
			}
			if fault == "partial-output" {
				require.EqualValues(t, 127, spliced)
			}
		})
	}
}

func TestSpliceFailureDoesNotLeakDescriptors(t *testing.T) {
	before, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	for i := 0; i < 50; i++ {
		source, err := os.CreateTemp(t.TempDir(), "input")
		require.NoError(t, err)
		dest, err := os.CreateTemp(t.TempDir(), "output")
		require.NoError(t, err)
		_, err = source.Write([]byte("fixture"))
		require.NoError(t, err)
		_, err = source.Seek(0, 0)
		require.NoError(t, err)
		_, err = copyFileWithSplice(context.Background(), dest, source, func(int, *int64, int, *int64, int, int) (int64, error) { return -1, unix.EIO })
		require.True(t, errors.Is(err, unix.EIO))
		require.NoError(t, source.Close())
		require.NoError(t, dest.Close())
	}
	after, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	require.Len(t, after, len(before))
}
