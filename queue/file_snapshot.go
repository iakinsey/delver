package queue

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func walkQueueDirectory(path string, visit func(os.DirEntry) error) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	for {
		entries, err := dir.ReadDir(256)
		for _, entry := range entries {
			if e := visit(entry); e != nil {
				return e
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// Counts and oldest age share one bounded walk. Only age queries stat files;
// metadata allocations never grow with the queue directory's entry count.
func (s *fileQueue) fileSnapshot(age bool) (map[string]int64, float64, error) {
	counts := map[string]int64{"pending": 0, "leased": 0, "failed": 0}
	oldest := time.Now()
	for at, root := range []string{s.path, s.dlqPath} {
		s.candidateScans.Add(1)
		err := walkQueueDirectory(root, func(entry os.DirEntry) error {
			name := entry.Name()
			state := "pending"
			if strings.HasSuffix(name, claimedSuffix) {
				state = "leased"
				name = strings.TrimSuffix(name, claimedSuffix)
			}
			if !s.entityRegex.MatchString(name) {
				return nil
			}
			if at == 1 {
				state = "failed"
			}
			counts[state]++
			if age && at == 0 {
				info, e := os.Stat(filepath.Join(root, entry.Name()))
				if os.IsNotExist(e) {
					return nil
				}
				if e != nil {
					return e
				}
				if info.ModTime().Before(oldest) {
					oldest = info.ModTime()
				}
			}
			return nil
		})
		if err != nil {
			return nil, 0, err
		}
	}
	return counts, time.Since(oldest).Seconds(), nil
}
