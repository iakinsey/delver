package queue

import (
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const fileCandidateLimit = 256

type fileCandidate struct {
	name  string
	order [3]uint64
}

func candidateFromName(name string) fileCandidate {
	c := fileCandidate{name: name}
	remaining := name
	for i := range c.order {
		part, next, _ := strings.Cut(remaining, "-")
		c.order[i], _ = strconv.ParseUint(part, 10, 64)
		remaining = next
	}
	return c
}
func (a fileCandidate) less(b fileCandidate) bool {
	for i := range a.order {
		if a.order[i] != b.order[i] {
			return a.order[i] < b.order[i]
		}
	}
	return a.name < b.name
}
func (s *fileQueue) invalidateCandidatesLocked() {
	s.candidates = nil
	s.candidatesReady = false
	s.candidatesMore = false
}
func (s *fileQueue) offerCandidateLocked(name string) {
	if !s.candidatesReady || !s.entityRegex.MatchString(name) {
		return
	}
	c := candidateFromName(name)
	// Uncached disk entries may precede a new low-priority enqueue. Refill
	// before admitting that enqueue beyond the cache's known boundary.
	if s.candidatesMore && len(s.candidates) < fileCandidateLimit && (len(s.candidates) == 0 || s.candidates[len(s.candidates)-1].less(c)) {
		return
	}
	at := sort.Search(len(s.candidates), func(i int) bool { return !s.candidates[i].less(c) })
	if at < len(s.candidates) && s.candidates[at].name == name {
		return
	}
	if at >= fileCandidateLimit {
		s.candidatesMore = true
		return
	}
	if len(s.candidates) == fileCandidateLimit {
		s.candidatesMore = true
	}
	if len(s.candidates) < fileCandidateLimit {
		s.candidates = append(s.candidates, fileCandidate{})
	}
	copy(s.candidates[at+1:], s.candidates[at:])
	s.candidates[at] = c
}
func (s *fileQueue) refillCandidatesLocked() error {
	dir, e := os.Open(s.path)
	if e != nil {
		return e
	}
	defer dir.Close()
	s.candidates = nil
	s.candidatesReady = true
	s.candidatesMore = false
	for {
		entries, err := dir.ReadDir(256)
		for _, entry := range entries {
			s.offerCandidateLocked(entry.Name())
		}
		if err != nil {
			if err != io.EOF {
				s.invalidateCandidatesLocked()
				return err
			}
			break
		}
	}
	s.candidateScans.Add(1)
	s.candidatesUntil = time.Now().Add(maxPollDelay(s.maxPollDelay))
	return nil
}
func (s *fileQueue) notifyPendingLocked(name string) {
	s.offerCandidateLocked(name)
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *fileQueue) Performance() map[string]int64 {
	s.putLock.Lock()
	defer s.putLock.Unlock()
	return map[string]int64{"directory_scans": s.candidateScans.Load(), "cached_candidates": int64(len(s.candidates))}
}

func (s *fileQueue) pendingCandidateAvailable() (bool, error) {
	s.putLock.Lock()
	defer s.putLock.Unlock()
	if !s.candidatesReady || len(s.candidates) == 0 && s.candidatesMore || time.Now().After(s.candidatesUntil) {
		if err := s.refillCandidatesLocked(); err != nil {
			return false, err
		}
	}
	return len(s.candidates) > 0, nil
}
