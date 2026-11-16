package application

import (
	"github.com/iakinsey/delver/flow"
	"github.com/iakinsey/delver/frontier"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

func (p *preparedApplication) pressure() flow.Pressure {
	pressure := flow.Pressure{Queues: map[string]int64{}, OldestSeconds: map[string]float64{}, MemoryMB: flow.HeapMB()}
	for _, w := range p.app.Workers {
		if w.Type != "composite_extractor" && w.Type != "frontier_accumulator" && w.Type != "jsonl_sink" && w.Type != "transformer" {
			continue
		}
		resource := p.resources[w.Inbox]
		if q, ok := resource.(interface {
			Pressure() (map[string]int64, float64, error)
		}); ok {
			counts, age, e := q.Pressure()
			if e != nil {
				pressure.Health = "queue unavailable: " + w.Inbox
			} else {
				pressure.Queues[w.Inbox] = counts["pending"] + counts["leased"]
				pressure.OldestSeconds[w.Inbox] = age
			}
			continue
		}
		if q, ok := resource.(interface {
			Counts() (map[string]int64, error)
		}); ok {
			counts, e := q.Counts()
			if e != nil {
				pressure.Health = "queue unavailable: " + w.Inbox
			} else {
				pressure.Queues[w.Inbox] = counts["pending"] + counts["leased"]
			}
		}
	}
	// Snapshot storage paths before the controller starts. Copying the whole
	// application here races with HTTP client wiring during startup.
	for _, path := range p.pressurePaths {
		free, err := diskFreeMB(path)
		if err != nil {
			pressure.Health = "disk capacity unavailable"
			continue
		}
		if !pressure.DiskKnown || free < pressure.DiskFreeMB {
			pressure.DiskFreeMB = free
		}
		pressure.DiskKnown = true
	}

	var usage syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &usage) == nil {
		now := time.Now()
		cpu := float64(usage.Utime.Sec+usage.Stime.Sec) + float64(usage.Utime.Usec+usage.Stime.Usec)/1e6
		p.pressureMu.Lock()
		if !p.pressureTime.IsZero() {
			pressure.CPUPercent = (cpu - p.pressureCPU) / now.Sub(p.pressureTime).Seconds() / float64(runtime.NumCPU()) * 100
		}
		p.pressureCPU = cpu
		p.pressureTime = now
		p.pressureMu.Unlock()
	}
	seen := map[string]bool{}
	for _, worker := range p.app.Workers {
		if worker.Type != "http_fetcher" || seen[worker.Inbox] {
			continue
		}
		seen[worker.Inbox] = true
		if q, ok := p.resources[worker.Inbox].(interface {
			Counts() (map[string]int64, error)
		}); ok {
			if counts, e := q.Counts(); e == nil {
				pressure.FetchPending += counts["pending"]
			} else {
				pressure.Health = "fetch queue unavailable: " + worker.Inbox
			}
		}
	}

	for _, resource := range p.resources {
		if _, ok := resource.(frontier.FrontierStore); ok {
			if source, ok := resource.(interface {
				Counts() (map[string]int64, error)
			}); ok {
				if counts, e := source.Counts(); e == nil {
					pressure.Frontier = counts
				} else {
					pressure.Health = "frontier counts unavailable"
				}
			}
		}

		if h, ok := resource.(interface{ Health() string }); ok {
			if e := h.Health(); e != "" {
				pressure.Health = e
			}
		}
	}
	return pressure
}

// Missing output files use their closest existing parent on the same mount.
func diskFreeMB(path string) (float64, error) {
	path = filepath.Clean(path)
	for {
		var fs syscall.Statfs_t
		if err := syscall.Statfs(path, &fs); err == nil {
			return float64(fs.Bavail) * float64(fs.Bsize) / (1 << 20), nil
		} else if !os.IsNotExist(err) {
			return 0, err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return 0, os.ErrNotExist
		}
		path = parent
	}
}
