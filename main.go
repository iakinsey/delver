package main

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"os/signal"
	"reflect"
	"sync"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/resource/bloom"
	"github.com/iakinsey/delver/resource/logger"
	"github.com/iakinsey/delver/resource/maps"
	"github.com/iakinsey/delver/resource/objectstore"
	"github.com/iakinsey/delver/worker"
	"github.com/iakinsey/delver/worker/accumulator"
	"github.com/iakinsey/delver/worker/extractor"
	"github.com/iakinsey/delver/worker/fetcher"
	"github.com/iakinsey/delver/worker/publisher"
)

const terminationWaitTime = 5 * time.Second

func main() {
	if len(os.Args) <= 1 {
		log.Fatalf("Config path must be provided")
	}

	StartFromJsonConfig(os.Args[1])
}

func StartFromJsonConfig(path string) {
	app := config.Application{}
	inter := config.RawApplication{}
	f, err := os.Open(path)

	if err != nil {
		log.Fatalf("failed to open config: %s", path)
	}

	b, err := ioutil.ReadAll(f)

	if err != nil {
		log.Fatalf("failed to read config: %s", path)
	}

	if err := json.Unmarshal(b, &inter); err != nil {
		log.Fatalf("failed to parse intermediate config: %s", path)
	}

	config.Set(inter.Config)

	if err := json.Unmarshal(b, &app); err != nil {
		log.Fatalf("falsed to parse config: %s", path)
	}

	StartFromApplication(app)
}

func StartFromApplication(app config.Application) {
	resources := CreateResources(app.Resources)
	workers := CreateWorkers(app.Workers, resources)

	StartApplication(app, resources, workers)
	AwaitTermination(resources, workers)
}

func StartApplication(app config.Application, resources map[string]interface{}, workers map[string]worker.WorkerManager) {
	for _, resource := range resources {
		if q, ok := resource.(queue.Queue); ok {
			go q.Start()
		}
	}

	for _, wc := range app.Workers {
		manager := workers[wc.Name]
		count := wc.Count

		// Defaults to 0
		if count == 0 {
			count = 1
		}

		for i := 0; i < count; i++ {
			go manager.Start()
		}
	}
}

func AwaitTermination(resources map[string]interface{}, workers map[string]worker.WorkerManager) {
	done := make(chan bool)
	sigterm := make(chan os.Signal, 1)
	signal.Notify(sigterm, syscall.SIGTERM)

	<-sigterm

	Terminate(resources, workers, done)

	select {
	case <-time.After(terminationWaitTime):
		os.Exit(1)
	case <-done:
		os.Exit(0)
	}
}

func Terminate(resources map[string]interface{}, workers map[string]worker.WorkerManager, done chan bool) {
	var wg sync.WaitGroup

	for _, resource := range resources {
		if q, ok := resource.(queue.Queue); ok {
			wg.Add(1)

			go func() {
				defer wg.Done()
				q.Stop()
			}()
		}
	}

	for _, m := range workers {
		wg.Add(1)

		go func(m worker.WorkerManager) {
			defer wg.Done()
			m.Stop()
		}(m)
	}

	wg.Wait()

	done <- true
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
		case "fixed_seed_publisher":
			fsp := publisher.FixedSeedPublisherParams{}
			parseParamWithResources(wc.Parameters, &fsp, resources)
			w = publisher.NewFixedSeedPublisher(fsp)
		default:
			log.Fatalf("unknown worker type %s", wc.Type)
		}

		result[wc.Name] = GetWorkerManager(wc, resources, w)
	}

	return result
}

func GetWorkerManager(wc config.Worker, resources map[string]interface{}, w worker.Worker) (m worker.WorkerManager) {
	inbox, ok := resources[wc.Inbox]

	if !ok && wc.Manager != "job" {
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
		case "file_queue":
			fqp := queue.FileQueueParams{Resilient: true}
			parseParam(c.Parameters, &fqp)
			result[c.Name] = queue.NewFileQueue(fqp)
		case "timer":
			tp := queue.TimerQueueParams{}
			parseParam(c.Parameters, &tp)
			result[c.Name] = queue.NewTimerQueue(tp)
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

	rType := reflect.TypeOf(config)
	rValue := reflect.ValueOf(config)
	valelem := rType.Elem()

	for i := 0; i < valelem.NumField(); i++ {
		field := valelem.Field(i)

		if resourceTag, ok := field.Tag.Lookup("resource"); ok && resourceTag != "" {
			resource := getResource(data, config, resourceTag, resources)
			f := reflect.New(reflect.TypeOf(resource))

			f.Elem().Set(reflect.ValueOf(resource))
			rValue.Elem().Field(i).Set(f.Elem())
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

	var res string

	if err := json.Unmarshal(r, &res); err != nil {
		log.Fatalf("failed to parse resource value %s", key)
	}

	return res
}
