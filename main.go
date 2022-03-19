package main

import (
	"encoding/json"
	"os"
	"reflect"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/resource/bloom"
	"github.com/iakinsey/delver/resource/logger"
	"github.com/iakinsey/delver/resource/maps"
	"github.com/iakinsey/delver/resource/objectstore"
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

	osp := objectstore.FilesystemObjectStoreParams{Path: objectStorePath}
	objectStore := objectstore.NewFilesystemObjectStore(osp)
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

	fetch := fetcher.NewHttpFetcher(fetcher.HttpFetcherParams{
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
	urlStore := maps.NewMultiHostMap(maps.MultiHostMapParams{
		BasePath: urlStorePath,
	})
	visitedUrls := bloom.NewRollingBloomFilter(bloom.RollingBloomFilterParams{
		BloomCount: 3,
		MaxN:       10000,
		P:          1,
		Path:       visitedUrlsPath,
	})

	dbap := accumulator.DfsBasicAccumulatorParams{
		UrlStore:    urlStore,
		VisitedUrls: visitedUrls,
		MaxDepth:    maxDepth,
	}
	accum := accumulator.NewDfsBasicAccumulator(dbap)
	visitedDomains := maps.NewPersistentMap(maps.PersistentMapParams{Path: visitedDomainsPath})
	pub := publisher.NewDfsBasicPublisher(publisher.DfsBasicPublisherParams{
		OutputQueue:  fetcherInputQueue,
		UrlStorePath: urlStorePath,
		VisitedHosts: visitedDomains,
		RotateAfter:  rotateAfter,
		Robots:       r,
	})

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

func FromApplication(app config.Application) {

}

func CreateQueues(queueConfigs []config.Resource) map[string]queue.Queue {
	result := make(map[string]queue.Queue)

	for _, qc := range queueConfigs {
		switch qc.Type {
		case "file":
			fqp := queue.FileQueueParams{}
			parseParam(qc.Parameters, &fqp)
			result[qc.Name] = queue.NewFileQueue(fqp)
		case "timer":
			tp := queue.TimerQueueParams{}
			parseParam(qc.Parameters, &tp)
			result[qc.Name] = queue.NewTimerQueue(tp)
		default:
			log.Fatalf("unknown queue type %s", qc.Type)
		}
	}

	return result
}

func CreateWorkers(workerConfigs []config.Worker, resources map[string]interface{}) map[string]worker.WorkerManager {
	result := make(map[string]worker.WorkerManager)

	for _, wc := range workerConfigs {
		var w worker.Worker

		switch wc.Type {
		case "dfs_basic_accumulator":
			dbap := accumulator.DfsBasicAccumulatorParams{}
			parseParamWithResources(wc.Parameters, &dbap, resources)
			w = accumulator.NewDfsBasicAccumulator(dbap)
		case "news_accumulator":
			nap := accumulator.NewsAccumulatorParams{}
			parseParamWithResources(wc.Parameters, &nap, resources)
			w = accumulator.NewNewsAccumulator(nap)
		case "resource_accumulator":
			rap := accumulator.ResourceAccumulatorParams{}
			parseParamWithResources(wc.Parameters, &rap, resources)
			w = accumulator.NewResourceAccumulator(rap)
		case "composite_extractor":
			cap := extractor.CompositeArgs{}
			parseParamWithResources(wc.Parameters, &cap, resources)
			w = extractor.NewCompositeExtractorWorker(cap)
		case "http_fetcher":
			hfp := fetcher.HttpFetcherParams{}
			parseParamWithResources(wc.Parameters, &hfp, resources)
			w = fetcher.NewHttpFetcher(hfp)
		case "dfs_basic_publisher":
			dbp := publisher.DfsBasicPublisherParams{}
			parseParamWithResources(wc.Parameters, &dbp, resources)
			w = publisher.NewDfsBasicPublisher(dbp)
		case "rss_feed_publisher":
			rfp := publisher.RssFeedPublisherParams{}
			parseParamWithResources(wc.Parameters, &rfp, resources)
			w = publisher.NewRssFeedPublisher(rfp)
		default:
			log.Fatalf("unknown worker type %s", wc.Type)
		}

		result[wc.Name] = GetWorkerManager(wc, resources, w)
	}

	return result
}

func GetWorkerManager(wc config.Worker, resources map[string]interface{}, w worker.Worker) (m worker.WorkerManager) {
	inbox, ok := resources[wc.Inbox]

	if !ok {
		log.Fatalf("worker %s has no inbox %s", wc.Name, wc.Inbox)
	}

	outbox, ok := resources[wc.Outbox]

	if !ok {
		log.Fatalf("worker %s has no outbox %s", wc.Name, wc.Outbox)
	}

	switch wc.Manager {
	case "worker":
	case "":
		m = worker.NewWorkerManager(
			w,
			inbox.(queue.Queue),
			outbox.(queue.Queue),
		)
	case "job":
		m = worker.NewJobManager(
			w,
			outbox.(queue.Queue),
			wc.Interval,
		)
	default:
		log.Fatalf("unknown worker manager: %s", wc.Manager)
	}

	return
}

func CreateResources(configs []config.Resource) map[string]interface{} {
	result := make(map[string]interface{})

	for _, c := range configs {
		switch c.Type {
		case "bloom_filter":
			bfp := bloom.BloomFilterParams{}
			parseParam(c.Parameters, &bfp)
			result[c.Name] = bloom.NewBloomFilter(bfp)
		case "rolling_bloom_filter":
			rbfp := bloom.RollingBloomFilterParams{}
			parseParam(c.Parameters, &rbfp)
			result[c.Name] = bloom.NewRollingBloomFilter(rbfp)
		case "persistent_map":
			pmp := maps.PersistentMapParams{}
			parseParam(c.Parameters, &pmp)
			result[c.Name] = maps.NewPersistentMap(pmp)
		case "multi_host_map":
			mhmp := maps.MultiHostMapParams{}
			parseParam(c.Parameters, &mhmp)
			result[c.Name] = maps.NewMultiHostMap(mhmp)
		case "filesystem_object_store":
			fosp := objectstore.FilesystemObjectStoreParams{}
			parseParam(c.Parameters, &fosp)
			result[c.Name] = objectstore.NewFilesystemObjectStore(fosp)
		case "hdfs_logger":
			hlp := logger.HDFSLoggerParams{}
			parseParam(c.Parameters, &hlp)
			result[c.Name] = logger.NewHDFSLogger(hlp)
		case "elasticsearch_logger":
			elp := logger.ElasticsearchLoggerParams{}
			parseParam(c.Parameters, &elp)
			result[c.Name] = logger.NewElasticsearchLogger(elp)
		default:
			log.Fatalf("unknown resource %s", c.Type)
		}
	}

	return result
}

func parseParam(data []byte, config interface{}) {
	if err := json.Unmarshal(data, config); err != nil {
		log.Fatalf("failed to parse queue object ")
	}
}

func parseParamWithResources(data []byte, config interface{}, resources map[string]interface{}) {
	parseParam(data, config)

	values := reflect.TypeOf(config)
	elem := reflect.ValueOf(config).Elem()

	for i := 0; i < values.NumField(); i++ {
		field := values.Field(i)

		if resourceTag, ok := field.Tag.Lookup("resource"); ok && resourceTag != "" {
			resource := getResource(data, config, resourceTag, resources)
			field := reflect.New(reflect.TypeOf(resource))

			field.Elem().Set(reflect.ValueOf(resource))
			elem.Field(i).Set(field)
		}
	}
}

func getResource(data []byte, config interface{}, resourceKey string, resources map[string]interface{}) interface{} {
	resourceName := getResourceName(data, resourceKey)
	resource, ok := resources[resourceName]

	if !ok {
		log.Fatalf("resource %s not defined", resourceName)
	}

	return resource
}

func getResourceName(data []byte, key string) string {
	m := make(map[string]json.RawMessage)

	if err := json.Unmarshal(data, &m); err != nil {
		log.Fatalf("failed to parse resource json")
	}

	r, ok := m[key]

	if !ok {
		log.Fatalf("failed to find resource key %s", key)
	}

	return string(r)
}
