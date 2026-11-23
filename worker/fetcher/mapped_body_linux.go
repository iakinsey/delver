//go:build linux

package fetcher

import (
	"github.com/iakinsey/delver/flow"
	"os"

	"golang.org/x/sys/unix"
)

func newMappedBody(size int64) (*mappedBody, error) {
	if size <= 0 || int64(int(size)) != size {
		return nil, errMappedBodyUnavailable
	}
	fd, err := unix.MemfdCreate("delver-response", unix.MFD_CLOEXEC)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "delver-response")
	if err = file.Truncate(size); err != nil {
		file.Close()
		return nil, err
	}
	data, err := unix.Mmap(fd, 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		file.Close()
		return nil, err
	}
	releaseMemory := flow.TrackMappedMemory(size)
	return &mappedBody{file: file, data: data, release: func() { _ = unix.Munmap(data); _ = file.Close(); releaseMemory() }}, nil
}
