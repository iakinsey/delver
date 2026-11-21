package report

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestReceiptsDistinctAcrossReplayRestartAndFailures(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "report.sqlite")
	s, e := Open(path, false)
	require.NoError(t, e)
	records := []URL{{URI: "https://a.test/a#fragment"}, {URI: "https://a.test/a"}, {URI: "https://a.test/not-found"}, {URI: "https://a.test/timeout"}, {URI: "http://a.test:81/a"}, {URI: "https://b.test/target", Origin: "https://a.test/a"}, {URI: "https://b.test/target", Origin: "https://c.test/other"}}
	require.NoError(t, s.Discover(ctx, records))
	require.NoError(t, s.Discover(ctx, records))
	for _, r := range []struct {
		uri  string
		code int
	}{{"https://a.test/a", 200}, {"https://a.test/not-found", 404}, {"https://a.test/timeout", 0}, {"https://a.test/a", 503}, {"https://b.test/target", 302}} {
		require.NoError(t, s.Response(ctx, r.uri, r.code))
	}
	c, e := s.Counts(ctx)
	require.NoError(t, e)
	require.EqualValues(t, 5, c.Known)
	require.EqualValues(t, 3, c.Visited)
	require.EqualValues(t, 2, c.NotVisited)
	require.EqualValues(t, 3, c.Hosts)
	require.EqualValues(t, 2, c.Edges)
	require.NoError(t, s.Close())
	s, e = Open(path, false)
	require.NoError(t, e)
	defer s.Close()
	again, e := s.Counts(ctx)
	require.NoError(t, e)
	require.Equal(t, c, again)
	rows, e := s.Search(ctx, "a.test", "", 20)
	require.NoError(t, e)
	require.Len(t, rows, 1)
	require.EqualValues(t, 4, rows[0].Known)
	require.NoError(t, s.Discover(ctx, []URL{{URI: "https://b.test/target", Origin: "https://d.test/other"}}))
	again, e = s.Counts(ctx)
	require.NoError(t, e)
	require.EqualValues(t, 3, again.Edges)
}
func TestConcurrentReceiptsAndSearchBoundaries(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "report.sqlite"), false)
	require.NoError(t, e)
	defer s.Close()
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if e := s.Response(ctx, fmt.Sprintf("https://h%03d.test/page", j), 200); e != nil {
					t.Error(e)
				}
			}
		}()
	}
	wg.Wait()
	c, e := s.Counts(ctx)
	require.NoError(t, e)
	require.EqualValues(t, 10, c.Known)
	require.EqualValues(t, 10, c.Visited)
	first, e := s.Search(ctx, "h", "", 3)
	require.NoError(t, e)
	require.Len(t, first, 3)
	second, e := s.Search(ctx, "h", first[2].Name, 3)
	require.NoError(t, e)
	require.Len(t, second, 3)
	require.NotEqual(t, first[0].Name, second[0].Name)
	empty, e := s.Search(ctx, "%", "", 20)
	require.NoError(t, e)
	require.Empty(t, empty)
}
func TestNormalizeHostIdentity(t *testing.T) {
	for _, tc := range []struct{ in, url, host string }{{"HTTPS://WWW.Example.COM.:443/a#x", "https://www.example.com/a", "www.example.com"}, {"http://[::1]:80/", "http://[::1]/", "::1"}, {"https://é.test/", "https://xn--9ca.test/", "xn--9ca.test"}, {"https://a.test:8080/", "https://a.test:8080/", "a.test"}} {
		u, h, e := Normalize(tc.in)
		require.NoError(t, e)
		require.Equal(t, tc.url, u)
		require.Equal(t, tc.host, h)
	}
	for _, raw := range []string{"ftp://a.test/", "https://u:p@a.test/", "garbage"} {
		_, _, e := Normalize(raw)
		require.Error(t, e)
	}
}

// Runtime discovery, historical import and report reads open the same ledger
// independently. A busy writer must not make a sibling discovery fail.
func TestIndependentHandlesSerializeWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.sqlite")
	a, e := Open(path, false)
	require.NoError(t, e)
	defer a.Close()
	b, e := Open(path, true)
	require.NoError(t, e)
	defer b.Close()
	ctx := context.Background()
	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- a.batch(ctx, func(tx *sql.Tx) error {
			if _, err := recordURL(ctx, tx, "https://held.test/", false); err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	sibling := make(chan error, 1)
	go func() { sibling <- b.Response(ctx, "https://live.test/", 200) }()
	select {
	case err := <-sibling:
		close(release)
		<-done
		t.Fatalf("sibling write escaped writer serialization: %v", err)
	case <-time.After(5500 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-done)
	require.NoError(t, <-sibling)
	counts, e := a.Counts(ctx)
	require.NoError(t, e)
	require.EqualValues(t, 2, counts.Known)
	require.EqualValues(t, 1, counts.Visited)
}

func TestLiveReceiptsAndReadHandlesDuringBackfill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "import.sqlite")
	live, e := Open(path, false)
	require.NoError(t, e)
	defer live.Close()
	history, e := Open(path, true)
	require.NoError(t, e)
	defer history.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	wg.Add(3)
	go func() {
		defer wg.Done()
		for batch := 0; batch < 16; batch++ {
			records := make([]URL, 2048)
			for i := range records {
				records[i] = URL{URI: fmt.Sprintf("https://history.test/%d", batch*2048+i)}
			}
			if e := history.Discover(ctx, records); e != nil {
				errs <- e
				return
			}
			if e := history.Checkpoint(ctx, "cursor", fmt.Sprint(batch)); e != nil {
				errs <- e
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			if e := live.Response(ctx, fmt.Sprintf("https://live.test/%d", i), 200); e != nil {
				errs <- e
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			reader, e := Open(path, false)
			if e != nil {
				errs <- e
				return
			}
			_, e = reader.Counts(ctx)
			reader.Close()
			reader.Close()
			if e != nil {
				errs <- e
				return
			}
		}
	}()
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	counts, e := live.Counts(ctx)
	require.NoError(t, e)
	require.EqualValues(t, 32868, counts.Known)
	require.EqualValues(t, 100, counts.Visited)
}

func TestSearchRanksDistinctNeighborsAndPagesByRank(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "ranked.sqlite"), false)
	require.NoError(t, e)
	defer s.Close()
	ctx := context.Background()
	records := []URL{{URI: "https://isolated.test/"}}
	for _, peer := range []string{"a", "b", "c"} {
		records = append(records, URL{URI: "https://" + peer + ".test/", Origin: "https://z-hub.test/"})
		records = append(records, URL{URI: "https://" + peer + ".test/", Origin: "https://y-out.test/"})
	}
	for _, peer := range []string{"a", "b", "c", "d"} {
		records = append(records, URL{URI: "https://zz-in.test/", Origin: "https://" + peer + ".test/"})
	}
	// Reciprocal, repeated and same-host links must not inflate the score.
	for i := 0; i < 100; i++ {
		records = append(records, URL{URI: fmt.Sprintf("https://z-hub.test/%d", i), Origin: "https://a.test/"}, URL{URI: fmt.Sprintf("https://z-hub.test/self/%d", i), Origin: "https://z-hub.test/"})
	}
	require.NoError(t, s.Discover(ctx, records))
	all, e := s.Search(ctx, "", "", 100)
	require.NoError(t, e)
	require.Equal(t, "zz-in.test", all[0].Name)
	require.EqualValues(t, 4, all[0].Connections)
	require.Equal(t, "a.test", all[1].Name)
	require.EqualValues(t, 3, all[1].Connections)
	for i := 1; i < len(all); i++ {
		require.True(t, all[i-1].Connections > all[i].Connections || all[i-1].Connections == all[i].Connections && all[i-1].Name < all[i].Name)
	}
	require.Equal(t, "isolated.test", all[len(all)-1].Name)
	var names []string
	cursor := ""
	for {
		page, e := s.Search(ctx, "", cursor, 2)
		require.NoError(t, e)
		if len(page) == 0 {
			break
		}
		for _, h := range page {
			names = append(names, h.Name)
			require.NotEmpty(t, h.Cursor)
		}
		cursor = page[len(page)-1].Cursor
	}
	expected := []string{}
	for _, h := range all {
		expected = append(expected, h.Name)
	}
	require.Equal(t, expected, names)
	prefix, e := s.Search(ctx, "z", "", 100)
	require.NoError(t, e)
	require.Len(t, prefix, 2)
	require.Equal(t, "zz-in.test", prefix[0].Name)
	require.Equal(t, "z-hub.test", prefix[1].Name)
	require.EqualValues(t, 3, prefix[1].Connections)
	legacy, e := s.Search(ctx, "", all[1].Name, 2)
	require.NoError(t, e)
	require.Equal(t, all[2:4], legacy)
	// A cursor carries its original score, even if its host gains connections.
	first, e := s.Search(ctx, "", "", 2)
	require.NoError(t, e)
	require.NoError(t, s.Discover(ctx, []URL{{URI: "https://new-1.test/", Origin: "https://a.test/"}, {URI: "https://new-2.test/", Origin: "https://a.test/"}}))
	second, e := s.Search(ctx, "", first[1].Cursor, 100)
	require.NoError(t, e)
	for _, h := range second {
		require.NotEqual(t, first[0].Name, h.Name)
		require.NotEqual(t, first[1].Name, h.Name)
	}
	for _, after := range []string{"connections:no:host.test", "connections:-1:host.test", "connections:1:", "connections:9223372036854775808:host.test"} {
		_, e = s.Search(ctx, "", after, 2)
		require.Error(t, e)
	}
	require.NoError(t, s.Close())
	s, e = Open(s.path, false)
	require.NoError(t, e)
	defer s.Close()
	restored, e := s.Search(ctx, "z", "", 2)
	require.NoError(t, e)
	require.Equal(t, prefix, restored)
}
