package publisher

import (
	"encoding/json"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/resource/maps"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestDfsBasic(t *testing.T) {
	paths := testutil.SetupWorkerQueueFolders("DfsBasic")

	defer testutil.TeardownWorkerQueueFolders(paths)

	queues := testutil.CreateQueueTriad(paths)
	urlStorePath := util.NewTempPath("dfsBasicUrlStore")
	visitedDomainsPath := util.NewTempPath("dfsBasicVisitedDomains")
	rotateAfter := 1 * time.Millisecond
	mapper := maps.NewMultiHostMap(maps.MultiHostMapParams{BasePath: urlStorePath})
	urls := []string{
		"http://example.com/1",
		"http://example.com/2",
		"http://example.com/3",
		"http://example.com/4",
	}

	for _, u := range urls {
		req := message.FetcherRequest{
			URI: u,
		}

		if val, err := json.Marshal(req); err != nil {
			log.Fatalf("error preparing request: %s", err)
		} else {
			assert.NoError(t, mapper.Set([]byte(u), val))
		}
	}

	mapper.Close()

	conf := config.Get()
	visitedHosts := maps.NewPersistentMap(maps.PersistentMapParams{Path: visitedDomainsPath})
	robots := frontier.NewMemoryRobots(conf.Robots, util.NewHTTPClient(config.HTTPClientConfig{}))
	publisher := NewDfsBasicPublisher(DfsBasicPublisherParams{
		OutputQueue:  queues.Outbox,
		UrlStorePath: urlStorePath,
		VisitedHosts: visitedHosts,
		RotateAfter:  rotateAfter,
		Robots:       robots,
	})
	out, err := publisher.OnMessage(types.Message{})

	assert.Nil(t, out)
	assert.NoError(t, err)
	testutil.AssertFolderSize(t, paths.Outbox, len(urls))
}
