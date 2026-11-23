//go:build linux

package objectstore

import (
	"context"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

type spliceCall func(int, *int64, int, *int64, int, int) (int64, error)

func copyFileBody(ctx context.Context, destination, source *os.File) (int64, error) {
	return copyFileWithSplice(ctx, destination, source, unix.Splice)
}

func spliceUnsupported(err error) bool {
	return errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EXDEV) || errors.Is(err, unix.EPERM)
}

func copyFileWithSplice(ctx context.Context, destination, source *os.File, splice spliceCall) (int64, error) {
	var pipe [2]int
	if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC); err != nil {
		return 0, copyFileBuffered(ctx, destination, source)
	}
	input := os.NewFile(uintptr(pipe[0]), "object-splice-reader")
	output := os.NewFile(uintptr(pipe[1]), "object-splice-writer")
	defer input.Close()
	defer output.Close()
	// Increasing capacity is optional; user/kernel pipe quotas can reject it.
	_, _ = unix.FcntlInt(output.Fd(), unix.F_SETPIPE_SZ, 64<<10)
	var copied int64
	for {
		if err := ctx.Err(); err != nil {
			return copied, err
		}
		n, err := splice(int(source.Fd()), nil, pipe[1], nil, 64<<10, 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			if spliceUnsupported(err) {
				return copied, copyFileBuffered(ctx, destination, source)
			}
			return copied, err
		}
		if n == 0 {
			return copied, nil
		}
		for pending := n; pending > 0; {
			if err := ctx.Err(); err != nil {
				return copied, err
			}
			written, err := splice(pipe[0], nil, int(destination.Fd()), nil, int(pending), 0)
			if written > 0 {
				copied += written
				pending -= written
			}
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if err != nil {
				if !spliceUnsupported(err) {
					return copied, err
				}
				// The source offset already advanced. Drain exactly the uncommitted
				// pipe bytes before switching to buffered copying of the remainder.
				_, drainErr := io.CopyN(destination, contextReader{ctx, input}, pending)
				if drainErr != nil {
					return copied, drainErr
				}
				return copied, copyFileBuffered(ctx, destination, source)
			}
			if written == 0 {
				return copied, io.ErrNoProgress
			}
		}
	}
}
