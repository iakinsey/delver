package queue

import (
	"github.com/stretchr/testify/require"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCrawlStatusAfterClose(t *testing.T) {
	q, err := NewCrawlQueue(CrawlQueueParams{Path: filepath.Join(t.TempDir(), "crawl"), CrawlID: "closed", MaxAttempts: 1})
	require.NoError(t, err)
	s := q.(*crawlQueue)
	require.NoError(t, s.Close())
	_, err = s.Counts()
	require.Error(t, err)
	require.EqualValues(t, -1, s.Len())
}

func TestCrawlCloseWaitsForDatabaseReaders(t *testing.T) {
	q, err := NewCrawlQueue(CrawlQueueParams{Path: filepath.Join(t.TempDir(), "crawl"), CrawlID: "reader", MaxAttempts: 1})
	require.NoError(t, err)
	s := q.(*crawlQueue)
	s.dbMu.RLock()
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case <-closed:
		t.Fatal("database closed with a reader in progress")
	case <-time.After(20 * time.Millisecond):
	}
	s.dbMu.RUnlock()
	require.NoError(t, <-closed)
	_, err = s.Counts()
	require.Error(t, err)
}
func TestCrawlMonitoringConcurrentWithClose(t *testing.T) {
	for cycle := 0; cycle < 20; cycle++ {
		q, err := NewCrawlQueue(CrawlQueueParams{Path: filepath.Join(t.TempDir(), "crawl"), CrawlID: "concurrent-close", MaxAttempts: 1})
		require.NoError(t, err)
		s := q.(*crawlQueue)
		msg := crawlRequest(t, "http://example.test/")
		require.NoError(t, s.Put(msg, 0))
		require.NoError(t, s.Start())
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for j := 0; j < 30; j++ {
					s.Counts()
					s.Len()
					s.Failed(0, 50)
					s.ReferencedObjects()
					s.LoadOutputs(msg)
				}
			}()
		}
		close(start)
		require.NoError(t, s.Close())
		wg.Wait()
	}
}
