package flow

import (
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
)

var mappedMemory atomic.Int64

// TrackMappedMemory keeps off-heap response staging visible to memory gates.
// The full mapping is charged conservatively, including not-yet-resident pages.
func TrackMappedMemory(bytes int64) func() {
	mappedMemory.Add(bytes)
	var released atomic.Bool
	return func() {
		if released.CompareAndSwap(false, true) {
			mappedMemory.Add(-bytes)
		}
	}
}

func PressureMemoryMB() float64 {
	return HeapMB() + float64(mappedMemory.Load())/(1<<20)
}

// Default memory pressure thresholds leave room for several independent stores.
// Resolve to concrete MiB values so saved crawls do not change after restart.
func defaultMemoryMB() int {
	total := uint64(0)
	var info syscall.Sysinfo_t
	if syscall.Sysinfo(&info) == nil {
		total = info.Totalram * uint64(info.Unit)
	}
	for _, path := range []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"} {
		if raw, err := os.ReadFile(path); err == nil {
			if limit, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64); err == nil && limit > 0 && (total == 0 || limit < total) {
				total = limit
			}
		}
	}
	high := int(total / (4 << 20))
	if high == 0 || high > 4096 {
		high = 4096
	}
	if high < 64 {
		high = 64
	}
	return high
}
