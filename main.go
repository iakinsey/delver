package main

import (
	"encoding/json"
	"os"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/gateway/objectstore"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/util/testutil"
	"github.com/iakinsey/delver/worker"
	"github.com/iakinsey/delver/worker/accumulator"
	"github.com/iakinsey/delver/worker/extractor"
	"github.com/iakinsey/delver/worker/fetcher"
	"github.com/iakinsey/delver/worker/publisher"
)

func main() {
	//runtime.GOMAXPROCS(runtime.NumCPU() * 8)
	conf := config.Get()
	urlStorePath := util.MakeTempFolder("urlStorePath")
	visitedUrlsPath := util.NewTempPath("visitedUrls")
	objectStorePath := util.MakeTempFolder("objectStore")
	visitedDomainsPath := util.MakeTempFolder("visitedDomains")
	maxDepth := 2
	rotateAfter := 5 * time.Minute

	defer os.RemoveAll(urlStorePath)
	defer os.RemoveAll(objectStorePath)
	defer os.RemoveAll(visitedDomainsPath)
	defer os.Remove(visitedUrlsPath)

	objectStore, err := objectstore.NewFilesystemObjectStore(objectStorePath)

	if err != nil {
		log.Fatalf(err.Error())
	}

	httpClient := util.NewHTTPClient(conf.HTTPClient)
	r := frontier.NewMemoryRobots(conf.Robots, httpClient)

	fetcherInputQueue, inbox, dlq := testutil.CreateFileQueue("fetcherInput")
	defer os.RemoveAll(inbox)
	defer os.RemoveAll(dlq)

	fetcherOutputQueue, inbox, dlq := testutil.CreateFileQueue("fetcherOutput")
	defer os.RemoveAll(inbox)
	defer os.RemoveAll(dlq)

	compositeOutputQueue, inbox, dlq := testutil.CreateFileQueue("compositeOutput")
	defer os.RemoveAll(inbox)
	defer os.RemoveAll(dlq)

	fetch := fetcher.NewHttpFetcher(fetcher.HttpFetcherArgs{
		MaxRetries:  2,
		ObjectStore: objectStore,
		Client:      httpClient,
	})
	comp := extractor.NewCompositeExtractorWorker(extractor.CompositeArgs{
		ObjectStore: objectStore,
		Enabled: []string{
			message.UrlExtractor,
			message.AdversarialExtractor,
			message.CompanyNameExtractor,
			message.CountryExtractor,
			message.LanguageExtractor,
			message.SentimentExtractor,
			message.TextExtractor,
		},
	})
	accum := accumulator.NewDfsBasicAccumulator(urlStorePath, visitedUrlsPath, maxDepth)
	pub := publisher.NewDfsBasicPublisher(
		fetcherInputQueue,
		urlStorePath,
		visitedDomainsPath,
		rotateAfter,
		r,
	)

	fetchManager := worker.NewWorkerManager(fetch, fetcherInputQueue, fetcherOutputQueue)
	compManager := worker.NewWorkerManager(comp, fetcherOutputQueue, compositeOutputQueue)
	accumManager := worker.NewWorkerManager(accum, compositeOutputQueue, fetcherInputQueue)
	pubManager := worker.NewJobManager(pub, fetcherInputQueue, 1*time.Minute)

	message, _ := json.Marshal(message.FetcherRequest{
		RequestID: types.NewV4(),
		URI:       "http://en.wikipedia.org/wiki",
		Protocol:  types.ProtocolHTTP,
	})

	fetcherInputQueue.Put(types.Message{
		ID:          "0-0-0-TestName",
		MessageType: types.FetcherRequestType,
		Message:     json.RawMessage(message),
	}, 0)

	go fetcherInputQueue.Start()
	go fetcherOutputQueue.Start()
	go compositeOutputQueue.Start()

	for i := 0; i < conf.WorkerCounts; i++ {
		go fetchManager.Start()
		go compManager.Start()
		go accumManager.Start()
		go pubManager.Start()
	}

	select {}
}
