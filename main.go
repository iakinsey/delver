package main

import (
	"encoding/json"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/iakinsey/delver/gateway/robots"
	"github.com/iakinsey/delver/gateway/streamstore"
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
	runtime.GOMAXPROCS(runtime.NumCPU() * 8)

	urlStorePath := util.MakeTempFolder("urlStorePath")
	visitedUrlsPath := util.NewTempPath("visitedUrls")
	streamStorePath := util.MakeTempFolder("streamStore")
	visitedDomainsPath := util.MakeTempFolder("visitedDomains")
	maxDepth := 2
	rotateAfter := 5 * time.Minute

	defer os.RemoveAll(urlStorePath)
	defer os.RemoveAll(streamStorePath)
	defer os.RemoveAll(visitedDomainsPath)
	defer os.Remove(visitedUrlsPath)

	streamStore, err := streamstore.NewFilesystemStreamStore(streamStorePath)

	if err != nil {
		log.Fatalf(err.Error())
	}

	httpClient := util.NewHTTPClient(util.HTTPClientParams{
		Timeout:   2 * time.Minute,
		UserAgent: "delver-pre-alpha",
	})
	r := robots.NewMemoryRobots(httpClient)

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
		StreamStore: streamStore,
		Client:      httpClient,
	})
	comp := extractor.NewCompositeExtractorWorker(extractor.CompositeArgs{
		StreamStore: streamStore,
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
	go fetchManager.Start()
	go compManager.Start()
	go accumManager.Start()
	go pubManager.Start()

	select {}
}
