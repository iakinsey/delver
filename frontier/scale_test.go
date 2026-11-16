//go:build go1.19
// +build go1.19

package frontier

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func residentBytes() uint64 {
	b, _ := os.ReadFile("/proc/self/status")
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			f := strings.Fields(line)
			n, _ := strconv.ParseUint(f[1], 10, 64)
			return n * 1024
		}
	}
	return 0
}

// Opt-in because this writes roughly a gigabyte per backend. Database content
// exceeds the reopened process's resident set; the OS page cache is uncontrolled.
func TestFrontierScale(t *testing.T) {
	if os.Getenv("DELVER_FRONTIER_SCALE") != "1" {
		t.Skip("set DELVER_FRONTIER_SCALE=1 for the scale workload")
	}
	old := debug.SetMemoryLimit(96 << 20)
	defer debug.SetMemoryLimit(old)
	for _, engine := range []string{"badger", "sqlite"} {
		t.Run(engine, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "db")
			db, err := openWorkload(engine, path)
			if err != nil {
				t.Fatal(err)
			}
			const records = 262144
			// Distinct high-entropy payload defeats compression and models detailed history.
			payload := make([]byte, 4096)
			var state uint64 = 1
			started := time.Now()
			for start := 0; start < records; start += 64 {
				values := map[string][]byte{}
				for i := start; i < start+64; i++ {
					for j := range payload {
						state ^= state << 13
						state ^= state >> 7
						state ^= state << 17
						payload[j] = byte(state)
					}
					values[fmt.Sprintf("u/history/%012d", i)] = append([]byte(nil), payload...)
				}
				if err := db.put(values, nil); err != nil {
					db.close()
					t.Fatal(err)
				}
			}
			admission := time.Since(started)
			for i := 0; i < 128; i++ {
				key := fmt.Sprintf("p/%012d", i)
				if err := db.put(map[string][]byte{key: []byte(key)}, nil); err != nil {
					db.close()
					t.Fatal(err)
				}
			}
			if err := db.close(); err != nil {
				t.Fatal(err)
			}
			runtime.GC()
			debug.FreeOSMemory()
			reopen := time.Now()
			db, err = openWorkload(engine, path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.close()
			reopenElapsed := time.Since(reopen)
			rss := residentBytes()
			disk := diskUsage(root)
			if rss == 0 || uint64(disk) <= rss {
				t.Fatalf("dataset must exceed process resident set: disk=%d rss=%d", disk, rss)
			}
			durations := make([]time.Duration, 1000)
			for i := range durations {
				start := time.Now()
				keys, err := db.scan("p/", 32)
				durations[i] = time.Since(start)
				if err != nil || len(keys) != 32 {
					t.Fatal(keys, err)
				}
			}
			for i := 0; i < 1024; i++ {
				key := fmt.Sprintf("u/history/%012d", i*251)
				if _, err := db.get(key); err != nil {
					t.Fatal(err)
				}
			}
			// Bounded compaction/deletion batches, followed by cursor-safe reads.
			clean := time.Now()
			for start := 0; start < 4096; start += 64 {
				deletes := []string{}
				for i := start; i < start+64; i++ {
					deletes = append(deletes, fmt.Sprintf("u/history/%012d", i))
				}
				if err := db.put(nil, deletes); err != nil {
					t.Fatal(err)
				}
			}
			sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)
			result := map[string]interface{}{"backend": engine, "history_records": records, "record_bytes": 4096, "disk_bytes": disk, "resident_after_reopen": rss, "resident_after_workload": residentBytes(), "heap_after_workload": mem.HeapAlloc, "admission_seconds": admission.Seconds(), "admission_records_per_second": float64(records) / admission.Seconds(), "seek_p95_us": float64(durations[950].Nanoseconds()) / 1000, "reopen_ms": float64(reopenElapsed.Microseconds()) / 1000, "cleanup_4096_ms": float64(time.Since(clean).Microseconds()) / 1000}
			b, _ := json.Marshal(result)
			t.Log(string(b))
		})
	}
}
