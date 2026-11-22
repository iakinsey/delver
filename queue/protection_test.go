package queue

import (
	"context"
	"encoding/json"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/types"
	"github.com/stretchr/testify/require"
	"io"
	"path/filepath"
	"testing"
)

func TestCrawlQueueProtectionPurgeAndWriteFence(t *testing.T) {
	ctx := context.Background()
	q, e := NewCrawlQueue(CrawlQueueParams{Path: filepath.Join(t.TempDir(), "queue"), CrawlID: "test", MaxAttempts: 3})
	require.NoError(t, e)
	defer q.(io.Closer).Close()
	makeMessage := func(uri string) types.Message {
		raw, _ := json.Marshal(map[string]interface{}{"uri": uri, "links": []string{"http://blocked.test/link", "http://good.test/link"}})
		return types.Message{ID: string(types.NewV4()), MessageType: types.FetcherRequestType, Message: raw}
	}
	require.NoError(t, q.Put(makeMessage("http://blocked.test/page"), 0))
	require.NoError(t, q.Put(makeMessage("http://good.test/page"), 0))
	p, e := protection.Open(filepath.Join(t.TempDir(), "policy.sqlite"))
	require.NoError(t, e)
	defer p.Close()
	_, e = p.Trigger("http://blocked.test/", "synthetic", "crawl", "run", 0)
	require.ErrorIs(t, e, protection.ErrBlocked)
	native := q.(*crawlQueue)
	native.SetProtection(p)
	match := protection.NewMatcher(nil, []protection.Target{{Site: "blocked.test"}}).Matches
	require.Error(t, native.VerifyNoMatches(ctx, match))
	require.NoError(t, native.PurgeMatching(ctx, match))
	require.NoError(t, native.VerifyNoMatches(ctx, match))
	require.NoError(t, q.Put(makeMessage("http://blocked.test/new"), 0))
	require.NoError(t, q.Put(makeMessage("http://good.test/new"), 0))
	require.NoError(t, native.VerifyNoMatches(ctx, match))
}
